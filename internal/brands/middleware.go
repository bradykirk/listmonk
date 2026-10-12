package brands

import (
	"errors"
	"net/http"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v4"
)

const (
	// HeaderBrand is the request header that selects a brand.
	HeaderBrand  = "X-Listmonk-Brand"
	ctxBrand     = "ext.brand"
	ctxBrandList = "ext.brand_lists"

	// QueryBrand is the query parameter for requests that cannot carry a header:
	// browser downloads, iframe src, and form posts to an iframe.
	QueryBrand = "ext_brand"
)

// Service holds what the brand middleware needs.
type Service struct {
	Store *Store
	// AllPerms is the full permission catalog, given to a scoped super admin.
	AllPerms map[string]struct{}

	// load returns the brands and list tags for the send guard. Tests set it;
	// when nil, the guard reads them from Store.
	load func() ([]Brand, map[int][]string, error)
	// tplOwner replaces Store.EffectiveTemplateOwner in tests.
	tplOwner func(templateID int) (string, error)
}

// Middleware reads the selected brand, classifies the route, and for Scoped
// routes swaps in a narrowed user. It fails closed: an unclassified route with
// a brand selected is refused.
func (s *Service) Middleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		slug, err := RequestBrand(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if slug == "" {
			return next(c)
		}
		class, msg := Classify(c.Request().Method, c.Path())
		if class == Global {
			return next(c)
		}

		b, ok, err := s.Store.Get(slug)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "error loading brand")
		}
		if !ok {
			return echo.NewHTTPError(http.StatusBadRequest, "unknown brand")
		}
		switch class {
		case Scoped, BrandAware:
		case Refused:
			return echo.NewHTTPError(http.StatusBadRequest, msg)
		default: // Unclassified, or any unknown class value.
			return echo.NewHTTPError(http.StatusBadRequest, "this page does not support brand scope")
		}

		all, err := s.Store.All()
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "error loading brands")
		}
		tags, err := s.Store.ListTags()
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "error loading lists")
		}
		ids := BrandListIDs(b.Slug, tags, all)
		c.Set(ctxBrand, b)
		c.Set(ctxBrandList, ids)

		if class == Scoped {
			real, ok := c.Get(auth.UserHTTPCtxKey).(auth.User)
			if !ok {
				// No valid user: fail closed rather than run unscoped.
				return echo.NewHTTPError(http.StatusForbidden, "permission denied")
			}
			c.Set(auth.UserHTTPCtxKey, ScopeUser(real, ids, s.AllPerms))
		}
		return next(c)
	}
}

// RequestBrand returns the brand slug from the header, else from the query
// parameter. Both present with different values is an error.
func RequestBrand(r *http.Request) (string, error) {
	h := strings.TrimSpace(r.Header.Get(HeaderBrand))
	q := strings.TrimSpace(r.URL.Query().Get(QueryBrand))
	if h != "" && q != "" && h != q {
		return "", errors.New("brand header and brand parameter differ")
	}
	if h != "" {
		return h, nil
	}
	return q, nil
}

// BrandFrom returns the selected brand and its list IDs. false means All brands.
func BrandFrom(c echo.Context) (Brand, []int, bool) {
	b, ok := c.Get(ctxBrand).(Brand)
	if !ok {
		return Brand{}, nil, false
	}
	ids, _ := c.Get(ctxBrandList).([]int)
	return b, ids, true
}

// CheckRouteTable returns the /api/* routes that have no entry in the route table.
func (s *Service) CheckRouteTable(routes []*echo.Route) []string {
	var missing []string
	for _, r := range routes {
		// RouteNotFound handlers are stored as routes; they are not endpoints.
		if r.Method == echo.RouteNotFound {
			continue
		}
		if strings.HasPrefix(r.Path, "/api/") {
			if c, _ := Classify(r.Method, r.Path); c == Unclassified {
				missing = append(missing, r.Method+" "+r.Path)
			}
		}
	}
	return missing
}
