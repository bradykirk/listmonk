package brands

import (
	"math/rand"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
)

var allPerms = map[string]struct{}{}

func init() {
	for _, p := range []string{"lists:get_all", "lists:manage_all", "subscribers:get", "subscribers:get_all",
		"subscribers:manage", "subscribers:import", "subscribers:sql_query", "tx:send", "campaigns:get",
		"campaigns:get_all", "campaigns:get_analytics", "campaigns:manage", "campaigns:manage_all",
		"campaigns:send", "bounces:get", "bounces:manage", "media:get", "media:manage", "templates:get",
		"templates:manage", "settings:get", "settings:manage"} {
		allPerms[p] = struct{}{}
	}
}

func user(perms []string, get, manage []int) auth.User {
	u := auth.User{UserRoleID: 7, PermissionsMap: map[string]struct{}{}, ListPermissionsMap: map[int]map[string]struct{}{}}
	u.UserRole.ID = 7
	for _, p := range perms {
		u.PermissionsMap[p] = struct{}{}
	}
	for _, id := range get {
		u.GetListIDs = append(u.GetListIDs, id)
		ensure(u.ListPermissionsMap, id)["list:get"] = struct{}{}
	}
	for _, id := range manage {
		u.ManageListIDs = append(u.ManageListIDs, id)
		ensure(u.ListPermissionsMap, id)["list:manage"] = struct{}{}
	}
	return u
}

func ensure(m map[int]map[string]struct{}, id int) map[string]struct{} {
	if m[id] == nil {
		m[id] = map[string]struct{}{}
	}
	return m[id]
}

func TestScopeSuperAdmin(t *testing.T) {
	u := auth.User{UserRoleID: auth.SuperAdminRoleID}
	u.UserRole.ID = auth.SuperAdminRoleID
	s := ScopeUser(u, []int{3, 6}, allPerms)
	if s.UserRoleID == auth.SuperAdminRoleID || s.UserRole.ID == auth.SuperAdminRoleID {
		t.Fatal("still super admin")
	}
	for _, p := range []string{"lists:get_all", "lists:manage_all", "subscribers:get_all", "campaigns:get_all", "campaigns:manage_all"} {
		if s.HasPerm(p) {
			t.Fatalf("kept blanket %s", p)
		}
	}
	for _, p := range []string{"campaigns:get", "campaigns:manage", "subscribers:get", "settings:get"} {
		if !s.HasPerm(p) {
			t.Fatalf("lost %s", p)
		}
	}
	if s.HasListPerm(auth.PermTypeGet, 3) != nil || s.HasListPerm(auth.PermTypeManage, 6) != nil {
		t.Fatal("brand list denied")
	}
	if s.HasListPerm(auth.PermTypeGet, 17) == nil {
		t.Fatal("other brand list allowed")
	}
	if all, ids := s.GetPermittedLists(auth.PermTypeGet | auth.PermTypeManage); all || len(ids) != 2 {
		t.Fatal(all, ids)
	}
}

func TestScopeNeverEscalates(t *testing.T) {
	// Spec 5.2 / review round 2: a read-only user must not gain manage rights.
	u := user([]string{"subscribers:get"}, []int{3}, nil)
	s := ScopeUser(u, []int{3, 6}, allPerms)
	if s.HasListPerm(auth.PermTypeManage, 3) == nil {
		t.Fatal("gained manage on 3")
	}
	if s.HasListPerm(auth.PermTypeGet, 6) == nil {
		t.Fatal("gained get on 6")
	}
	if s.HasPerm("subscribers:manage") {
		t.Fatal("gained subscribers:manage")
	}
}

func TestScopeBlanketBecomesNarrow(t *testing.T) {
	u := user([]string{"campaigns:get_all", "lists:get_all"}, nil, nil)
	s := ScopeUser(u, []int{3}, allPerms)
	if !s.HasPerm("campaigns:get") || s.HasPerm("campaigns:manage") {
		t.Fatal("get_all must become get only")
	}
	if s.HasListPerm(auth.PermTypeGet, 3) != nil || s.HasListPerm(auth.PermTypeManage, 3) == nil {
		t.Fatal("lists:get_all must become get on brand lists only")
	}
}

func TestScopeDoesNotMutateRealUser(t *testing.T) {
	u := user([]string{"lists:get_all"}, nil, nil)
	_ = ScopeUser(u, []int{3}, allPerms)
	if !u.HasPerm("lists:get_all") || len(u.ListPermissionsMap) != 0 {
		t.Fatal("real user mutated")
	}
}

func TestScopeRandomRolesOnlyNarrow(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	names := make([]string, 0, len(allPerms))
	for p := range allPerms {
		names = append(names, p)
	}
	for i := 0; i < 2000; i++ {
		var perms []string
		for _, p := range names {
			if r.Intn(2) == 0 {
				perms = append(perms, p)
			}
		}
		var get, manage []int
		for id := 1; id <= 8; id++ {
			if r.Intn(3) == 0 {
				get = append(get, id)
			}
			if r.Intn(3) == 0 {
				manage = append(manage, id)
			}
		}
		u := user(perms, get, manage)
		s := ScopeUser(u, []int{1, 2, 3, 4}, allPerms)
		for p := range allPerms {
			if s.HasPerm(p) && !u.HasPerm(p) && !impliedBy(p, u) {
				t.Fatalf("iter %d gained %s", i, p)
			}
		}
		for id := 1; id <= 8; id++ {
			if s.HasListPerm(auth.PermTypeGet, id) == nil && u.HasListPerm(auth.PermTypeGet, id) != nil {
				t.Fatalf("iter %d gained get %d", i, id)
			}
			if s.HasListPerm(auth.PermTypeManage, id) == nil && u.HasListPerm(auth.PermTypeManage, id) != nil {
				t.Fatalf("iter %d gained manage %d", i, id)
			}
		}
	}
}

// impliedBy: X is allowed on the copy when the real user has X_all.
func impliedBy(p string, u auth.User) bool {
	return u.HasPerm(p + "_all")
}
