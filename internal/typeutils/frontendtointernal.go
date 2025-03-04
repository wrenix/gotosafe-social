// GoToSocial
// Copyright (C) GoToSocial Authors admin@gotosocial.org
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package typeutils

import (
	apimodel "github.com/superseriousbusiness/gotosocial/internal/api/model"
	"github.com/superseriousbusiness/gotosocial/internal/gtsmodel"
)

func APIVisToVis(m apimodel.Visibility) gtsmodel.Visibility {
	switch m {
	case apimodel.VisibilityPublic:
		return gtsmodel.VisibilityPublic
	case apimodel.VisibilityUnlisted:
		return gtsmodel.VisibilityUnlocked
	case apimodel.VisibilityPrivate:
		return gtsmodel.VisibilityFollowersOnly
	case apimodel.VisibilityMutualsOnly:
		return gtsmodel.VisibilityMutualsOnly
	case apimodel.VisibilityDirect:
		return gtsmodel.VisibilityDirect
	}
	return ""
}

func APIMarkerNameToMarkerName(m apimodel.MarkerName) gtsmodel.MarkerName {
	switch m {
	case apimodel.MarkerNameHome:
		return gtsmodel.MarkerNameHome
	case apimodel.MarkerNameNotifications:
		return gtsmodel.MarkerNameNotifications
	}
	return ""
}

func APIFilterActionToFilterAction(m apimodel.FilterAction) gtsmodel.FilterAction {
	switch m {
	case apimodel.FilterActionWarn:
		return gtsmodel.FilterActionWarn
	case apimodel.FilterActionHide:
		return gtsmodel.FilterActionHide
	}
	return gtsmodel.FilterActionNone
}
