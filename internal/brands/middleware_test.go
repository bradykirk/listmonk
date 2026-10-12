package brands

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
)

func TestMiddleware(t *testing.T) {
	db := testDB(t)
	Bootstrap(db)
	st := NewStore(db)
	st.Upsert(Brand{Slug: "ri", Name: "RI", ListTags: []string{"ri"}, Messengers: []string{"email-ri"}, FromDomains: []string{"rangeinsiders.com"}})
	db.MustExec(`INSERT INTO lists (uuid, name, type, optin, tags) VALUES (gen_random_uuid(),'a','private','single','{ri}'), (gen_random_uuid(),'b','private','single','{ta}')`)
	svc := &Service{Store: st, AllPerms: allPerms}

	super := auth.User{UserRoleID: auth.SuperAdminRoleID}
	super.UserRole.ID = auth.SuperAdminRoleID

	run := func(method, path, brand string) (int, echo.Context) {
		e := echo.New()
		req := httptest.NewRequest(method, "/", nil)
		if brand != "" {
			req.Header.Set("X-Listmonk-Brand", brand)
		}
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath(path)
		c.Set(auth.UserHTTPCtxKey, super)
		err := svc.Middleware(func(c echo.Context) error { return c.NoContent(204) })(c)
		if he, ok := err.(*echo.HTTPError); ok {
			return he.Code, c
		}
		return rec.Code, c
	}

	if code, c := run("GET", "/api/lists", ""); code != 204 || c.Get("ext.brand") != nil {
		t.Fatal("no header must pass through")
	}
	if code, _ := run("GET", "/api/lists", "zz"); code != 400 {
		t.Fatal("unknown brand", code)
	}
	if code, _ := run("GET", "/api/new-upstream", "ri"); code != 400 {
		t.Fatal("unclassified must fail closed", code)
	}
	if code, _ := run("DELETE", "/api/subscribers/:id", "ri"); code != 400 {
		t.Fatal("refused", code)
	}
	if code, c := run("GET", "/api/settings", "ri"); code != 204 || c.Get("ext.brand") != nil {
		t.Fatal("global must ignore header")
	}

	code, c := run("GET", "/api/lists", "ri")
	if code != 204 {
		t.Fatal(code)
	}
	u := c.Get(auth.UserHTTPCtxKey).(auth.User)
	if u.UserRoleID == auth.SuperAdminRoleID {
		t.Fatal("scoped route kept super admin")
	}
	if _, ids := u.GetPermittedLists(auth.PermTypeGet); len(ids) != 1 {
		t.Fatal(ids)
	}

	code, c = run("GET", "/api/templates", "ri")
	if code != 204 || c.Get(auth.UserHTTPCtxKey).(auth.User).UserRoleID != auth.SuperAdminRoleID {
		t.Fatal("brand-aware must keep the real user")
	}
	if b, ids, ok := BrandFrom(c); !ok || b.Slug != "ri" || len(ids) != 1 {
		t.Fatal(b, ids, ok)
	}
}

// Echo must hand the middleware the route pattern, not the raw URL.
func TestMiddlewareSeesRoutePattern(t *testing.T) {
	db := testDB(t)
	Bootstrap(db)
	st := NewStore(db)
	st.Upsert(Brand{Slug: "ri", Name: "RI", ListTags: []string{"ri"}})
	svc := &Service{Store: st, AllPerms: allPerms}

	e := echo.New()
	g := e.Group("", svc.Middleware)
	g.DELETE("/api/subscribers/:id", func(c echo.Context) error { return c.NoContent(204) })

	req := httptest.NewRequest("DELETE", "/api/subscribers/42", nil)
	req.Header.Set(HeaderBrand, "ri")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want refused 400 via route pattern, got %d", rec.Code)
	}
}

func TestMiddlewareDiffersAndQuery(t *testing.T) {
	db := testDB(t)
	Bootstrap(db)
	st := NewStore(db)
	st.Upsert(Brand{Slug: "ri", Name: "RI", ListTags: []string{"ri"}})
	svc := &Service{Store: st, AllPerms: allPerms}
	e := echo.New()
	e.GET("/api/lists", func(c echo.Context) error {
		_, _, ok := BrandFrom(c)
		if ok {
			return c.NoContent(204)
		}
		return c.NoContent(200)
	}, svc.Middleware)

	req := httptest.NewRequest("GET", "/api/lists?ext_brand=ri", nil)
	req.Header.Set(HeaderBrand, "ta")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatal("differ must be 400", rec.Code)
	}
}

func TestCheckRouteTable(t *testing.T) {
	svc := &Service{}
	got := svc.CheckRouteTable([]*echo.Route{
		{Method: "GET", Path: "/api/lists"},
		{Method: "GET", Path: "/api/new-upstream"},
		{Method: "GET", Path: "/health"},
	})
	if len(got) != 1 || got[0] != "GET /api/new-upstream" {
		t.Fatal(got)
	}
}

func TestRequestBrand(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/subscribers/export?ext_brand=ri", nil)
	if s, err := RequestBrand(r); err != nil || s != "ri" {
		t.Fatal(s, err)
	}
	r.Header.Set(HeaderBrand, "ri")
	if s, err := RequestBrand(r); err != nil || s != "ri" {
		t.Fatal(s, err)
	}
	r.Header.Set(HeaderBrand, "ta")
	if _, err := RequestBrand(r); err == nil {
		t.Fatal("header and parameter differ: want error")
	}
	r = httptest.NewRequest("GET", "/api/lists", nil)
	if s, err := RequestBrand(r); err != nil || s != "" {
		t.Fatal(s, err)
	}
}
