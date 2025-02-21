package structr

import (
	"fmt"
	"reflect"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"codeberg.org/gruf/go-mangler"
	"github.com/modern-go/reflect2"
)

// struct_field contains pre-prepared type
// information about a struct's field member,
// including memory offset and hash function.
type struct_field struct {

	// type2 contains the reflect2
	// type information for this field,
	// used in repacking it as eface.
	type2 reflect2.Type

	// offsets defines whereabouts in
	// memory this field is located.
	offsets []next_offset

	// struct field type mangling
	// (i.e. fast serializing) fn.
	mangle mangler.Mangler

	// zero value data, used when
	// nil encountered during ptr
	// offset following.
	zero unsafe.Pointer

	// mangled zero value string,
	// if set this indicates zero
	// values of field not allowed
	zerostr string
}

// next_offset defines a next offset location
// in a struct_field, first by the number of
// derefences required, then by offset from
// that final memory location.
type next_offset struct {
	derefs uint
	offset uintptr
}

// find_field will search for a struct field with given set of names,
// where names is a len > 0 slice of names account for struct nesting.
func find_field(t reflect.Type, names []string) (sfield struct_field) {
	var (
		// is_exported returns whether name is exported
		// from a package; can be func or struct field.
		is_exported = func(name string) bool {
			r, _ := utf8.DecodeRuneInString(name)
			return unicode.IsUpper(r)
		}

		// pop_name pops the next name from
		// the provided slice of field names.
		pop_name = func() string {
			name := names[0]
			names = names[1:]
			if !is_exported(name) {
				panicf("field is not exported: %s", name)
			}
			return name
		}

		// field is the iteratively searched
		// struct field value in below loop.
		field reflect.StructField
	)

	for len(names) > 0 {
		// Pop next name.
		name := pop_name()

		var off next_offset

		// Dereference any ptrs to struct.
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
			off.derefs++
		}

		// Check for valid struct type.
		if t.Kind() != reflect.Struct {
			panicf("field %s is not struct (or ptr-to): %s", t, name)
		}

		var ok bool

		// Look for next field by name.
		field, ok = t.FieldByName(name)
		if !ok {
			panicf("unknown field: %s", name)
		}

		// Set next offset value.
		off.offset = field.Offset
		sfield.offsets = append(sfield.offsets, off)

		// Set the next type.
		t = field.Type
	}

	// Get field type as reflect2.
	sfield.type2 = reflect2.Type2(t)

	// Find mangler for field type.
	sfield.mangle = mangler.Get(t)

	// Set possible zero value and its string.
	sfield.zero = sfield.type2.UnsafeNew()
	i := sfield.type2.UnsafeIndirect(sfield.zero)
	sfield.zerostr = string(sfield.mangle(nil, i))

	return
}

// extract_fields extracts given structfields from the provided value type,
// this is done using predetermined struct field memory offset locations.
func extract_fields(ptr unsafe.Pointer, fields []struct_field) []any {
	// Prepare slice of field ifaces.
	ifaces := make([]any, len(fields))
	for i, field := range fields {

		// loop scope.
		fptr := ptr

		for _, offset := range field.offsets {
			// Dereference any ptrs to offset.
			fptr = deref(fptr, offset.derefs)

			if fptr == nil {
				// Use zero value.
				fptr = field.zero
				break
			}

			// Jump forward by offset to next ptr.
			fptr = unsafe.Pointer(uintptr(fptr) +
				offset.offset)
		}

		// Repack value data ptr as empty interface.
		ifaces[i] = field.type2.UnsafeIndirect(fptr)
	}

	return ifaces
}

// deref will dereference ptr 'n' times (or until nil).
func deref(p unsafe.Pointer, n uint) unsafe.Pointer {
	for ; n > 0; n-- {
		if p == nil {
			return nil
		}
		p = *(*unsafe.Pointer)(p)
	}
	return p
}

// panicf provides a panic with string formatting.
func panicf(format string, args ...any) {
	panic(fmt.Sprintf(format, args...))
}
