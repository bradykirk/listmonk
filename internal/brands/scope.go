package brands

import (
	"encoding/json"

	"github.com/knadh/listmonk/internal/auth"
)

// blanket maps each blanket permission to the narrow permission it implies.
var blanket = map[string]string{
	auth.PermListGetAll:         "",
	auth.PermListManageAll:      "",
	auth.PermSubscribersGetAll:  auth.PermSubscribersGet,
	auth.PermCampaignsGetAll:    auth.PermCampaignsGet,
	auth.PermCampaignsManageAll: auth.PermCampaignsManage,
}

// ScopeUser returns a copy of u narrowed to brandLists (spec 5.2). The copy
// never holds a permission, list, or list right that u lacks. u is not changed.
func ScopeUser(u auth.User, brandLists []int, allPerms map[string]struct{}) auth.User {
	super := u.UserRoleID == auth.SuperAdminRoleID || u.UserRole.ID == auth.SuperAdminRoleID

	s := u
	s.UserRoleID, s.UserRole.ID = -1, -1
	s.PermissionsMap = map[string]struct{}{}
	src := u.PermissionsMap
	if super {
		src = allPerms
	}
	for p := range src {
		if narrow, isBlanket := blanket[p]; isBlanket {
			if narrow != "" {
				s.PermissionsMap[narrow] = struct{}{}
			}
			continue
		}
		s.PermissionsMap[p] = struct{}{}
	}
	s.UserRole.Permissions = make([]string, 0, len(s.PermissionsMap))
	for p := range s.PermissionsMap {
		s.UserRole.Permissions = append(s.UserRole.Permissions, p)
	}

	canGet := func(id int) bool {
		return super || u.HasPerm(auth.PermListGetAll) || u.HasListPerm(auth.PermTypeGet, id) == nil
	}
	canManage := func(id int) bool {
		return super || u.HasPerm(auth.PermListManageAll) || u.HasListPerm(auth.PermTypeManage, id) == nil
	}

	s.ListPermissionsMap = map[int]map[string]struct{}{}
	s.GetListIDs, s.ManageListIDs = []int{}, []int{}
	for _, id := range brandLists {
		m := map[string]struct{}{}
		if canGet(id) {
			m[auth.PermListGet] = struct{}{}
			s.GetListIDs = append(s.GetListIDs, id)
		}
		if canManage(id) {
			m[auth.PermListManage] = struct{}{}
			s.ManageListIDs = append(s.ManageListIDs, id)
		}
		if len(m) > 0 {
			s.ListPermissionsMap[id] = m
		}
	}
	return s
}

// FilterSubscriberLists keeps only the subscription objects whose id is in
// brandListIDs. It drops other brands' subscriptions from a subscriber response
// (spec 5.4). Input is the subscriber's lists JSON array.
func FilterSubscriberLists(raw []byte, brandListIDs []int) []byte {
	var lists []map[string]interface{}
	if len(raw) == 0 || json.Unmarshal(raw, &lists) != nil {
		return []byte("[]")
	}
	in := make(map[int]struct{}, len(brandListIDs))
	for _, id := range brandListIDs {
		in[id] = struct{}{}
	}
	out := make([]map[string]interface{}, 0, len(lists))
	for _, l := range lists {
		id, _ := l["id"].(float64)
		if _, ok := in[int(id)]; ok {
			out = append(out, l)
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return []byte("[]")
	}
	return b
}
