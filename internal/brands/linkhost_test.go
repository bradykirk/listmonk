package brands

import (
	"errors"
	"testing"

	"github.com/knadh/listmonk/models"
)

func TestNormalizeLinkHost(t *testing.T) {
	ok := map[string]string{
		"":                                  "",
		"  ":                                "",
		"https://News.TacticalAdvisor.net/": "https://news.tacticaladvisor.net",
		"https://news.tacticaladvisor.net":  "https://news.tacticaladvisor.net",
	}
	for in, want := range ok {
		got, err := NormalizeLinkHost(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"http://news.tacticaladvisor.net", "https://news.tacticaladvisor.net/x", "https://news.tacticaladvisor.net:8443",
		"https://", "javascript:alert(1)", "news.tacticaladvisor.net", "https://user@news.tacticaladvisor.net",
		"https://news.tacticaladvisor.net?x=1", "https://news.tacticaladvisor.net#f", "https://news tacticaladvisor.net",
		"https://localhost", "https://127.0.0.1",
	} {
		if _, err := NormalizeLinkHost(in); err == nil {
			t.Errorf("%q must be refused", in)
		}
	}
}

func linkSvc(ta string, loadErr error) *Service {
	b := guardBrands()
	b[1].LinkHost = ta
	return &Service{load: func() ([]Brand, map[int][]string, error) { return b, guardTags, loadErr }}
}

func lists(ids ...int) func(int) ([]int, error) {
	return func(int) ([]int, error) { return ids, nil }
}

func TestLinkHostFor(t *testing.T) {
	const host = "https://news.tacticaladvisor.net"
	c := func(messenger string) *models.Campaign {
		return &models.Campaign{Base: models.Base{ID: 9}, Messenger: messenger}
	}
	cases := []struct {
		name      string
		svc       *Service
		lists     func(int) ([]int, error)
		messenger string
		want      string
		wantErr   bool
	}{
		{"ta", linkSvc(host, nil), lists(17), "email-ta", host, false},
		{"ri", linkSvc(host, nil), lists(3, 6), "email-ri", "", false},
		{"ta, no host", linkSvc("", nil), lists(17), "email-ta", "", false},
		{"ta lists, fallback messenger", linkSvc(host, nil), lists(17), "email", "", true},
		{"ta lists, ri messenger", linkSvc(host, nil), lists(17), "email-ri", "", true},
		{"ta lists, fallback, no host", linkSvc("", nil), lists(17), "email", "", false},
		{"mixed brands", linkSvc(host, nil), lists(3, 17), "email-ta", "", true},
		{"unbranded", linkSvc(host, nil), lists(40), "email", "", false},
		{"no lists", linkSvc(host, nil), lists(), "email-ta", "", false},
		{"deleted list only", linkSvc(host, nil), lists(0), "email-ta", "", false},
		{"ta list + deleted list", linkSvc(host, nil), lists(17, 0, 999), "email-ta", host, false},
		{"no brand has a host, deleted list", linkSvc("", nil), lists(0), "email-ri", "", false},
		{"no brand has a host, mixed lists", linkSvc("", nil), lists(3, 17), "email-ri", "", false},
		{"no brand has a host, load ok", linkSvc("", nil), lists(17), "email", "", false},
		{"load error", linkSvc(host, errors.New("db down")), lists(17), "email-ta", "", true},
		{"lists error", linkSvc(host, nil), func(int) ([]int, error) { return nil, errors.New("db down") }, "email-ta", "", true},
	}
	for _, tc := range cases {
		got, err := tc.svc.LinkResolver(tc.lists)(c(tc.messenger))
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q, err=%v", tc.name, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestLinkResolverUsesCampaignID(t *testing.T) {
	var asked int
	r := linkSvc("https://news.tacticaladvisor.net", nil).LinkResolver(func(id int) ([]int, error) { asked = id; return []int{17}, nil })
	// A campaign from NextCampaigns carries no Lists; the resolver must look them up by ID.
	if _, err := r(&models.Campaign{Base: models.Base{ID: 42}, Messenger: "email-ta"}); err != nil || asked != 42 {
		t.Fatalf("asked %d, err %v", asked, err)
	}
}
