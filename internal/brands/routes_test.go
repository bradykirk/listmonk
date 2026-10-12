package brands

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func split(k string) (string, string) {
	i := strings.Index(k, " ")
	return k[:i], k[i+1:]
}

func TestClassify(t *testing.T) {
	cases := map[string]RouteClass{
		"GET /api/campaigns":                   Scoped,
		"PUT /api/campaigns/:id/status":        Scoped,
		"GET /api/subscribers":                 Scoped,
		"PUT /api/subscribers/lists":           Scoped,
		"DELETE /api/subscribers/:id":          Refused,
		"POST /api/subscribers/query/delete":   Refused,
		"PUT /api/subscribers/query/blocklist": Refused,
		"PUT /api/bounces/blocklist":           Refused,
		"GET /api/templates":                   BrandAware,
		"POST /api/lists":                      BrandAware,
		"GET /api/analytics/summary":           BrandAware,
		"GET /api/settings":                    Global,
		"POST /api/tx":                         Global,
		"GET /api/ext/brands":                  Global,
	}
	for k, want := range cases {
		m, p := split(k)
		if got, _ := Classify(m, p); got != want {
			t.Errorf("%s: got %v want %v", k, got, want)
		}
	}
	if _, msg := Classify("DELETE", "/api/subscribers"); msg != "Delete is a global action. Select All brands." {
		t.Fatal(msg)
	}
	if _, msg := Classify("PUT", "/api/subscribers/blocklist"); msg != "Blocklist is a global action. Select All brands." {
		t.Fatal(msg)
	}
}

func TestUnknownRouteIsUnclassified(t *testing.T) {
	if c, _ := Classify("GET", "/api/some/new/upstream/route"); c != Unclassified {
		t.Fatal(c)
	}
}

// Every Refused route has a message, and only Refused routes have one.
func TestRefusalMessages(t *testing.T) {
	for k, c := range Routes {
		_, has := refusal[k]
		if (c == Refused) != has {
			t.Errorf("%s: class %v, refusal message present=%v", k, c, has)
		}
	}
}

// Every /api/ route registered in cmd/handlers.go must be classified, and
// every entry in Routes must still be registered there.
func TestRoutesCoverHandlers(t *testing.T) {
	src, err := os.ReadFile("../../cmd/handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`\bg\.(GET|POST|PUT|PATCH|DELETE)\(\s*"(/api/[^"]*)"`)
	ms := re.FindAllStringSubmatch(string(src), -1)
	if len(ms) < 100 {
		t.Fatalf("regex found only %d registrations; handlers.go layout changed?", len(ms))
	}

	registered := map[string]bool{}
	for _, m := range ms {
		k := m[1] + " " + m[2]
		if registered[k] {
			t.Errorf("duplicate registration %s", k)
		}
		registered[k] = true
		if c, _ := Classify(m[1], m[2]); c == Unclassified {
			t.Errorf("route not classified: %s", k)
		}
	}
	for k := range Routes {
		if !registered[k] {
			t.Errorf("Routes has an entry not registered in handlers.go: %s", k)
		}
	}
}

// The automations API routes are BrandAware: the handlers filter by brand.
func TestAutomationRoutes(t *testing.T) {
	for _, k := range []string{
		"GET /api/ext/automations",
		"POST /api/ext/automations",
		"GET /api/ext/automations/:id",
		"PUT /api/ext/automations/:id",
		"DELETE /api/ext/automations/:id",
		"POST /api/ext/automations/enroll",
	} {
		m, p := split(k)
		if got, _ := Classify(m, p); got != BrandAware {
			t.Errorf("%s: got %v want BrandAware", k, got)
		}
	}
}
