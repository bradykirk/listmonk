package brands

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// serve answers /ext/link-host-check/<lookup> the way the public route does.
func serve(lc *LinkChecks) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, ok := lc.Answer(strings.TrimPrefix(r.URL.Path, LinkCheckPath))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(resp))
	}
}

func TestVerifyLinkHostThisInstance(t *testing.T) {
	lc := NewLinkChecks()
	srv := httptest.NewTLSServer(serve(lc))
	defer srv.Close()
	if err := lc.Verify(srv.URL, LinkCheckClient(srv.Client())); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyLinkHostRefusals(t *testing.T) {
	lc := NewLinkChecks()
	other := NewLinkChecks() // another Listmonk instance: its own registry
	cases := map[string]http.HandlerFunc{
		"other instance": serve(other),
		"reflects lookup": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(strings.TrimPrefix(r.URL.Path, LinkCheckPath)))
		},
		"200 other body": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"data":true}`)) },
		"404":            http.NotFound,
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://news.rangeinsiders.com"+r.URL.Path, http.StatusFound)
		},
		"slow": func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) },
	}
	for name, h := range cases {
		srv := httptest.NewTLSServer(h)
		c := LinkCheckClient(srv.Client())
		c.Timeout = 100 * time.Millisecond
		if err := lc.Verify(srv.URL, c); err == nil {
			t.Errorf("%s: accepted", name)
		}
		srv.Close()
	}
}

func TestVerifyLinkHostBadCertificate(t *testing.T) {
	lc := NewLinkChecks()
	srv := httptest.NewTLSServer(serve(lc))
	defer srv.Close()
	// The default client does not trust the test certificate.
	if err := lc.Verify(srv.URL, LinkCheckClient(nil)); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
}

func TestLinkChecksExpireAndSingleUse(t *testing.T) {
	now := time.Unix(1000, 0)
	lc := NewLinkChecks()
	lc.now = func() time.Time { return now }
	lookup, resp := lc.begin()
	if got, ok := lc.Answer(lookup); !ok || got != resp {
		t.Fatal("live token")
	}
	now = now.Add(31 * time.Second)
	if _, ok := lc.Answer(lookup); ok {
		t.Fatal("expired token answered")
	}
	if _, ok := lc.Answer(""); ok {
		t.Fatal("empty token answered")
	}
	l2, r2 := lc.begin()
	if l2 == lookup || r2 == resp || l2 == r2 || len(l2) != 64 {
		t.Fatalf("tokens: %s %s", l2, r2)
	}
}

func TestBrandRequestLinkHostPresence(t *testing.T) {
	var a, b BrandRequest
	if err := json.Unmarshal([]byte(`{"slug":"ta","name":"TA"}`), &a); err != nil || a.LinkHost != nil {
		t.Fatalf("absent: %v %v", a.LinkHost, err)
	}
	if err := json.Unmarshal([]byte(`{"slug":"ta","link_host":""}`), &b); err != nil || b.LinkHost == nil || *b.LinkHost != "" {
		t.Fatalf("empty: %v %v", b.LinkHost, err)
	}
}

func TestApplyLinkHost(t *testing.T) {
	str := func(s string) *string { return &s }
	var called []string
	ok := func(h string) error { called = append(called, h); return nil }
	fail := func(h string) error { called = append(called, h); return errors.New("not this instance") }
	var b Brand

	if err := ApplyLinkHost(&b, nil, "https://kept.example.net", fail); err != nil || b.LinkHost != "https://kept.example.net" {
		t.Fatalf("absent: %q %v", b.LinkHost, err)
	}
	if err := ApplyLinkHost(&b, str(""), "https://kept.example.net", fail); err != nil || b.LinkHost != "" {
		t.Fatalf("clear: %q %v", b.LinkHost, err)
	}
	if err := ApplyLinkHost(&b, str("https://kept.example.net/"), "https://kept.example.net", fail); err != nil || b.LinkHost != "https://kept.example.net" {
		t.Fatalf("unchanged: %q %v", b.LinkHost, err)
	}
	if len(called) != 0 {
		t.Fatalf("verify called for absent/clear/unchanged: %v", called)
	}
	if err := ApplyLinkHost(&b, str("https://News.Example.net/"), "", ok); err != nil || b.LinkHost != "https://news.example.net" || called[0] != "https://news.example.net" {
		t.Fatalf("new: %q %v %v", b.LinkHost, err, called)
	}
	b.LinkHost = ""
	if err := ApplyLinkHost(&b, str("https://other.example.net"), "", fail); err == nil {
		t.Fatal("a host that failed verification was accepted")
	}
	if err := ApplyLinkHost(&b, str("http://example.net"), "", ok); err == nil {
		t.Fatal("http accepted")
	}
}
