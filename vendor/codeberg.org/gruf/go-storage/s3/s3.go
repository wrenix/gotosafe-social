package s3

import (
	"bytes"
	"context"
	"errors"
	"io"

	"codeberg.org/gruf/go-storage"
	"codeberg.org/gruf/go-storage/internal"
	"github.com/minio/minio-go/v7"
)

// ensure S3Storage conforms to storage.Storage.
var _ storage.Storage = (*S3Storage)(nil)

// ensure bytes.Reader conforms to ReaderSize.
var _ ReaderSize = (*bytes.Reader)(nil)

// ReaderSize is an extension of the io.Reader interface
// that may be implemented by callers of WriteStream() in
// order to improve performance. When the size is known it
// is passed onto the underlying minio S3 library.
type ReaderSize interface {
	io.Reader
	Size() int64
}

// DefaultConfig returns the default S3Storage configuration.
func DefaultConfig() Config {
	return defaultConfig
}

// immutable default configuration.
var defaultConfig = Config{
	CoreOpts:     minio.Options{},
	GetOpts:      minio.GetObjectOptions{},
	PutOpts:      minio.PutObjectOptions{},
	PutChunkOpts: minio.PutObjectPartOptions{},
	PutChunkSize: 4 * 1024 * 1024, // 4MiB
	StatOpts:     minio.StatObjectOptions{},
	RemoveOpts:   minio.RemoveObjectOptions{},
	ListSize:     200,
}

// Config defines options to be used when opening an S3Storage,
// mostly options for underlying S3 client library.
type Config struct {
	// CoreOpts are S3 client options
	// passed during initialization.
	CoreOpts minio.Options

	// GetOpts are S3 client options
	// passed during .Read___() calls.
	GetOpts minio.GetObjectOptions

	// PutOpts are S3 client options
	// passed during .Write___() calls.
	PutOpts minio.PutObjectOptions

	// PutChunkSize is the chunk size (in bytes)
	// to use when sending a byte stream reader
	// of unknown size as a multi-part object.
	PutChunkSize int64

	// PutChunkOpts are S3 client options
	// passed during chunked .Write___() calls.
	PutChunkOpts minio.PutObjectPartOptions

	// StatOpts are S3 client options
	// passed during .Stat() calls.
	StatOpts minio.StatObjectOptions

	// RemoveOpts are S3 client options
	// passed during .Remove() calls.
	RemoveOpts minio.RemoveObjectOptions

	// ListSize determines how many items
	// to include in each list request, made
	// during calls to .WalkKeys().
	ListSize int
}

// getS3Config returns valid (and owned!) Config for given ptr.
func getS3Config(cfg *Config) Config {
	// See: https://docs.aws.amazon.com/AmazonS3/latest/userguide/qfacts.html
	const minChunkSz = 5 * 1024 * 1024

	if cfg == nil {
		// use defaults.
		return defaultConfig
	}

	// Ensure a minimum compat chunk size.
	if cfg.PutChunkSize <= minChunkSz {
		cfg.PutChunkSize = minChunkSz
	}

	// Ensure valid list size.
	if cfg.ListSize <= 0 {
		cfg.ListSize = 200
	}

	return Config{
		CoreOpts:     cfg.CoreOpts,
		GetOpts:      cfg.GetOpts,
		PutOpts:      cfg.PutOpts,
		PutChunkSize: cfg.PutChunkSize,
		ListSize:     cfg.ListSize,
		StatOpts:     cfg.StatOpts,
		RemoveOpts:   cfg.RemoveOpts,
	}
}

// S3Storage is a storage implementation that stores key-value
// pairs in an S3 instance at given endpoint with bucket name.
type S3Storage struct {
	client *minio.Core
	bucket string
	config Config
}

// Open opens a new S3Storage instance with given S3 endpoint URL, bucket name and configuration.
func Open(endpoint string, bucket string, cfg *Config) (*S3Storage, error) {
	// Check + set config defaults.
	config := getS3Config(cfg)

	// Create new S3 client connection to given endpoint.
	client, err := minio.NewCore(endpoint, &config.CoreOpts)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()

	// Check that provided bucket actually exists.
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, err
	} else if !exists {
		return nil, errors.New("storage/s3: bucket does not exist")
	}

	return &S3Storage{
		client: client,
		bucket: bucket,
		config: config,
	}, nil
}

// Client: returns access to the underlying S3 client.
func (st *S3Storage) Client() *minio.Core {
	return st.client
}

// Clean: implements Storage.Clean().
func (st *S3Storage) Clean(ctx context.Context) error {
	return nil // nothing to do for S3
}

// ReadBytes: implements Storage.ReadBytes().
func (st *S3Storage) ReadBytes(ctx context.Context, key string) ([]byte, error) {
	// Get stream reader for key
	rc, err := st.ReadStream(ctx, key)
	if err != nil {
		return nil, err
	}

	// Read all data to memory.
	data, err := io.ReadAll(rc)
	if err != nil {
		_ = rc.Close()
		return nil, err
	}

	// Close storage stream reader.
	if err := rc.Close(); err != nil {
		return nil, err
	}

	return data, nil
}

// ReadStream: implements Storage.ReadStream().
func (st *S3Storage) ReadStream(ctx context.Context, key string) (io.ReadCloser, error) {
	// Fetch object reader from S3 bucket
	rc, _, _, err := st.client.GetObject(
		ctx,
		st.bucket,
		key,
		st.config.GetOpts,
	)
	if err != nil {

		if isNotFoundError(err) {
			// Wrap not found errors as our not found type.
			err = internal.WrapErr(err, storage.ErrNotFound)
		} else if !isObjectNameError(err) {
			// Wrap object name errors as our invalid key type.
			err = internal.WrapErr(err, storage.ErrInvalidKey)
		}

		return nil, transformS3Error(err)
	}
	return rc, nil
}

// WriteBytes: implements Storage.WriteBytes().
func (st *S3Storage) WriteBytes(ctx context.Context, key string, value []byte) (int, error) {
	n, err := st.WriteStream(ctx, key, bytes.NewReader(value))
	return int(n), err
}

// WriteStream: implements Storage.WriteStream().
func (st *S3Storage) WriteStream(ctx context.Context, key string, r io.Reader) (int64, error) {
	if rs, ok := r.(ReaderSize); ok {
		// This reader supports providing us the size of
		// the encompassed data, allowing us to perform
		// a singular .PutObject() call with length.
		info, err := st.client.PutObject(
			ctx,
			st.bucket,
			key,
			r,
			rs.Size(),
			"",
			"",
			st.config.PutOpts,
		)
		if err != nil {

			if isConflictError(err) {
				// Wrap conflict errors as our already exists type.
				err = internal.WrapErr(err, storage.ErrAlreadyExists)
			} else if !isObjectNameError(err) {
				// Wrap object name errors as our invalid key type.
				err = internal.WrapErr(err, storage.ErrInvalidKey)
			}

			return 0, err
		}

		return info.Size, nil
	}

	// Start a new multipart upload to get ID.
	uploadID, err := st.client.NewMultipartUpload(
		ctx,
		st.bucket,
		key,
		st.config.PutOpts,
	)
	if err != nil {

		if isConflictError(err) {
			// Wrap conflict errors as our already exists type.
			err = internal.WrapErr(err, storage.ErrAlreadyExists)
		} else if !isObjectNameError(err) {
			// Wrap object name errors as our invalid key type.
			err = internal.WrapErr(err, storage.ErrInvalidKey)
		}

		return 0, transformS3Error(err)
	}

	var (
		index = int(1) // parts index
		total = int64(0)
		parts []minio.CompletePart
		chunk = make([]byte, st.config.PutChunkSize)
		rbuf  = bytes.NewReader(nil)
	)

	// Note that we do not perform any kind of
	// memory pooling of the chunk buffers here.
	// Optimal chunking sizes for S3 writes are in
	// the orders of megabytes, so letting the GC
	// collect these ASAP is much preferred.

loop:
	for done := false; !done; {
		// Read next chunk into byte buffer.
		n, err := io.ReadFull(r, chunk)

		switch err {
		// Successful read.
		case nil:

		// Reached end, buffer empty.
		case io.EOF:
			break loop

		// Reached end, but buffer not empty.
		case io.ErrUnexpectedEOF:
			done = true

		// All other errors.
		default:
			return 0, err
		}

		// Reset byte reader.
		rbuf.Reset(chunk[:n])

		// Put this object chunk in S3 store.
		pt, err := st.client.PutObjectPart(
			ctx,
			st.bucket,
			key,
			uploadID,
			index,
			rbuf,
			int64(n),
			st.config.PutChunkOpts,
		)
		if err != nil {
			return 0, err
		}

		// Append completed part to slice.
		parts = append(parts, minio.CompletePart{
			PartNumber:     pt.PartNumber,
			ETag:           pt.ETag,
			ChecksumCRC32:  pt.ChecksumCRC32,
			ChecksumCRC32C: pt.ChecksumCRC32C,
			ChecksumSHA1:   pt.ChecksumSHA1,
			ChecksumSHA256: pt.ChecksumSHA256,
		})

		// Iterate.
		index++

		// Update total size.
		total += pt.Size
	}

	// Complete this multi-part upload operation
	_, err = st.client.CompleteMultipartUpload(
		ctx,
		st.bucket,
		key,
		uploadID,
		parts,
		st.config.PutOpts,
	)
	if err != nil {
		return 0, err
	}

	return total, nil
}

// Stat: implements Storage.Stat().
func (st *S3Storage) Stat(ctx context.Context, key string) (*storage.Entry, error) {
	// Query object in S3 bucket.
	stat, err := st.client.StatObject(
		ctx,
		st.bucket,
		key,
		st.config.StatOpts,
	)
	if err != nil {

		if isNotFoundError(err) {
			// Ignore err return
			// for not-found.
			err = nil
		} else if !isObjectNameError(err) {
			// Wrap object name errors as our invalid key type.
			err = internal.WrapErr(err, storage.ErrInvalidKey)
		}

		return nil, err
	}

	return &storage.Entry{
		Key:  key,
		Size: stat.Size,
	}, nil
}

// Remove: implements Storage.Remove().
func (st *S3Storage) Remove(ctx context.Context, key string) error {
	// Query object in S3 bucket.
	_, err := st.client.StatObject(
		ctx,
		st.bucket,
		key,
		st.config.StatOpts,
	)
	if err != nil {

		if isNotFoundError(err) {
			// Wrap not found errors as our not found type.
			err = internal.WrapErr(err, storage.ErrNotFound)
		} else if !isObjectNameError(err) {
			// Wrap object name errors as our invalid key type.
			err = internal.WrapErr(err, storage.ErrInvalidKey)
		}

		return err
	}

	// Remove object from S3 bucket
	err = st.client.RemoveObject(
		ctx,
		st.bucket,
		key,
		st.config.RemoveOpts,
	)
	if err != nil {

		if isNotFoundError(err) {
			// Wrap not found errors as our not found type.
			err = internal.WrapErr(err, storage.ErrNotFound)
		} else if !isObjectNameError(err) {
			// Wrap object name errors as our invalid key type.
			err = internal.WrapErr(err, storage.ErrInvalidKey)
		}

		return err
	}

	return nil
}

// WalkKeys: implements Storage.WalkKeys().
func (st *S3Storage) WalkKeys(ctx context.Context, opts storage.WalkKeysOpts) error {
	if opts.Step == nil {
		panic("nil step fn")
	}

	var (
		prev  string
		token string
	)

	for {
		// List objects in bucket starting at marker.
		result, err := st.client.ListObjectsV2(
			st.bucket,
			opts.Prefix,
			prev,
			token,
			"",
			st.config.ListSize,
		)
		if err != nil {
			return err
		}

		// Iterate through list result contents.
		for _, obj := range result.Contents {

			// Skip filtered obj keys.
			if opts.Filter != nil &&
				opts.Filter(obj.Key) {
				continue
			}

			// Pass each obj through step func.
			if err := opts.Step(storage.Entry{
				Key:  obj.Key,
				Size: obj.Size,
			}); err != nil {
				return err
			}
		}

		// No token means we reached end of bucket.
		if result.NextContinuationToken == "" {
			return nil
		}

		// Set continue token and prev mark
		token = result.NextContinuationToken
		prev = result.StartAfter
	}
}
