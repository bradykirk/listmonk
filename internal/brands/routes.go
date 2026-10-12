package brands

// RouteClass says how brand scope applies to an API route.
type RouteClass int

const (
	// Unclassified is the zero value. A route missing from Routes gets this
	// class, and the caller must fail closed.
	Unclassified RouteClass = iota
	// Global routes ignore the brand header.
	Global
	// Scoped routes run as a scoped user with a campaign predicate.
	Scoped
	// BrandAware routes run as the real user. The handler filters by the
	// brand in the context.
	BrandAware
	// Refused routes are rejected when a brand is selected.
	Refused
)

const (
	msgDelete    = "Delete is a global action. Select All brands."
	msgBlocklist = "Blocklist is a global action. Select All brands."
)

// Routes maps "METHOD /pattern" (echo's c.Path()) to its class.
var Routes = map[string]RouteClass{
	// Global.
	"GET /api/health":                                   Global,
	"GET /api/config":                                   Global,
	"GET /api/lang/:lang":                               Global,
	"GET /api/settings":                                 Global,
	"PUT /api/settings":                                 Global,
	"PUT /api/settings/:key":                            Global,
	"POST /api/settings/smtp/test":                      Global,
	"POST /api/admin/reload":                            Global,
	"GET /api/logs":                                     Global,
	"GET /api/events":                                   Global,
	"GET /api/about":                                    Global,
	"GET /api/import/subscribers":                       Global,
	"GET /api/import/subscribers/logs":                  Global,
	"POST /api/import/subscribers":                      Global,
	"DELETE /api/import/subscribers":                    Global,
	"DELETE /api/maintenance/subscribers/:type":         Global,
	"DELETE /api/maintenance/analytics/:type":           Global,
	"GET /api/maintenance/analytics/:type/export":       Global,
	"DELETE /api/maintenance/subscriptions/unconfirmed": Global,
	"POST /api/tx":                                      Global,
	"GET /api/profile":                                  Global,
	"PUT /api/profile":                                  Global,
	"GET /api/users":                                    Global,
	"GET /api/users/:id":                                Global,
	"POST /api/users":                                   Global,
	"PUT /api/users/:id":                                Global,
	"DELETE /api/users":                                 Global,
	"DELETE /api/users/:id":                             Global,
	"POST /api/logout":                                  Global,
	"GET /api/users/:id/twofa/totp":                     Global,
	"PUT /api/users/:id/twofa":                          Global,
	"DELETE /api/users/:id/twofa":                       Global,
	"GET /api/roles/users":                              Global,
	"GET /api/roles/lists":                              Global,
	"POST /api/roles/users":                             Global,
	"POST /api/roles/lists":                             Global,
	"PUT /api/roles/users/:id":                          Global,
	"PUT /api/roles/lists/:id":                          Global,
	"DELETE /api/roles/:id":                             Global,

	// Public, unauthenticated endpoints.
	"GET /api/public/lists":          Global,
	"POST /api/public/subscription":  Global,
	"GET /api/public/captcha/altcha": Global,
	"GET /api/public/archive":        Global,

	// Brand management endpoints (added in Task 9).
	"GET /api/ext/brands":              Global,
	"POST /api/ext/brands":             Global,
	"PUT /api/ext/brands/:slug":        Global,
	"DELETE /api/ext/brands/:slug":     Global,
	"PUT /api/ext/templates/:id/brand": Global,
	"PUT /api/ext/media/:id/brand":     Global,
	"GET /api/ext/ownership":           Global,

	// Scoped: lists.
	"GET /api/lists":        Scoped,
	"GET /api/lists/:id":    Scoped,
	"PUT /api/lists/:id":    Scoped,
	"DELETE /api/lists":     Scoped,
	"DELETE /api/lists/:id": Scoped,

	// Scoped: subscribers.
	"GET /api/subscribers":                Scoped,
	"GET /api/subscribers/:id":            Scoped,
	"GET /api/subscribers/:id/activity":   Scoped,
	"GET /api/subscribers/:id/export":     Scoped,
	"GET /api/subscribers/:id/bounces":    Scoped,
	"DELETE /api/subscribers/:id/bounces": Scoped,
	"POST /api/subscribers":               Scoped,
	"PUT /api/subscribers/:id":            Scoped,
	"PATCH /api/subscribers/:id":          Scoped,
	"POST /api/subscribers/:id/optin":     Scoped,
	"PUT /api/subscribers/lists/:id":      Scoped,
	"PUT /api/subscribers/lists":          Scoped,
	"PUT /api/subscribers/query/lists":    Scoped,
	"GET /api/subscribers/export":         Scoped,

	// Scoped: campaigns.
	"GET /api/campaigns":                           Scoped,
	"GET /api/campaigns/running/stats":             Scoped,
	"GET /api/campaigns/:id":                       Scoped,
	"GET /api/campaigns/analytics/:type":           Scoped,
	"GET /api/campaigns/:id/analytics/summary":     Scoped,
	"GET /api/campaigns/:id/analytics/links":       Scoped,
	"GET /api/campaigns/:id/analytics/subscribers": Scoped,
	"GET /api/campaigns/:id/preview":               Scoped,
	"POST /api/campaigns/:id/preview/archive":      Scoped,
	"POST /api/campaigns/:id/preview":              Scoped,
	"POST /api/campaigns/:id/content":              Scoped,
	"POST /api/campaigns/:id/text":                 Scoped,
	"POST /api/campaigns/:id/test":                 Scoped,
	"POST /api/campaigns":                          Scoped,
	"PUT /api/campaigns/:id":                       Scoped,
	"PUT /api/campaigns/:id/status":                Scoped,
	"PUT /api/campaigns/:id/archive":               Scoped,
	"DELETE /api/campaigns":                        Scoped,
	"DELETE /api/campaigns/:id":                    Scoped,

	// BrandAware.
	"POST /api/lists":                      BrandAware,
	"GET /api/templates":                   BrandAware,
	"GET /api/templates/:id":               BrandAware,
	"GET /api/templates/:id/preview":       BrandAware,
	"POST /api/templates/preview":          BrandAware,
	"POST /api/templates":                  BrandAware,
	"PUT /api/templates/:id":               BrandAware,
	"PUT /api/templates/:id/default":       BrandAware,
	"DELETE /api/templates/:id":            BrandAware,
	"GET /api/media":                       BrandAware,
	"GET /api/media/:id":                   BrandAware,
	"POST /api/media":                      BrandAware,
	"DELETE /api/media/:id":                BrandAware,
	"GET /api/bounces":                     BrandAware,
	"GET /api/bounces/:id":                 BrandAware,
	"DELETE /api/bounces":                  BrandAware,
	"DELETE /api/bounces/:id":              BrandAware,
	"GET /api/dashboard/charts":            BrandAware,
	"GET /api/dashboard/counts":            BrandAware,
	"GET /api/dashboard/growth":            BrandAware,
	"GET /api/analytics/growth":            BrandAware,
	"GET /api/analytics/campaigns":         BrandAware,
	"GET /api/analytics/cohorts":           BrandAware,
	"GET /api/analytics/campaign-timeline": BrandAware,
	"GET /api/analytics/campaign-links":    BrandAware,
	"GET /api/analytics/activity":          BrandAware,
	"GET /api/analytics/summary":           BrandAware,
	"GET /api/analytics/domains":           BrandAware,
	"GET /api/analytics/send-times":        BrandAware,

	// Refused: delete.
	"DELETE /api/subscribers/:id":        Refused,
	"DELETE /api/subscribers":            Refused,
	"POST /api/subscribers/query/delete": Refused,

	// Refused: blocklist.
	"PUT /api/subscribers/blocklist":       Refused,
	"PUT /api/subscribers/:id/blocklist":   Refused,
	"PUT /api/subscribers/query/blocklist": Refused,
	"PUT /api/bounces/blocklist":           Refused,
}

// refusal holds the message for each Refused route.
var refusal = map[string]string{
	"DELETE /api/subscribers/:id":        msgDelete,
	"DELETE /api/subscribers":            msgDelete,
	"POST /api/subscribers/query/delete": msgDelete,

	"PUT /api/subscribers/blocklist":       msgBlocklist,
	"PUT /api/subscribers/:id/blocklist":   msgBlocklist,
	"PUT /api/subscribers/query/blocklist": msgBlocklist,
	"PUT /api/bounces/blocklist":           msgBlocklist,
}

// Classify returns the class of a route and, for Refused, the refusal
// message. path is echo's route pattern (c.Path()). An unknown route returns
// Unclassified.
func Classify(method, path string) (RouteClass, string) {
	k := method + " " + path
	c, ok := Routes[k]
	if !ok {
		return Unclassified, ""
	}
	return c, refusal[k]
}
