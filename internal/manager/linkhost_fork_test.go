package manager

import (
	"fmt"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/knadh/listmonk/models"
)

// FORK(hcm): per-brand link host. Plan: docs/superpowers/plans/2026-11-10-brand-link-host.md.

const riRoot = "https://news.rangeinsiders.com"

// urlCfg builds the six formats exactly as cmd/init.go initUrlConfig does.
func urlCfg(root string) Config {
	return Config{
		UnsubHeader:  true,
		RootURL:      root,
		UnsubURL:     fmt.Sprintf("%s/subscription/%%s/%%s", root),
		OptinURL:     fmt.Sprintf("%s/subscription/optin/%%s?%%s", root),
		LinkTrackURL: fmt.Sprintf("%s/link/%%s/%%s/%%s", root),
		MessageURL:   fmt.Sprintf("%s/campaign/%%s/%%s", root),
		ArchiveURL:   root + "/archive",
		ViewTrackURL: fmt.Sprintf("%s/campaign/%%s/%%s/px.png", root),
	}
}

func linkTestManager(cfg Config) *Manager {
	cfg.IndividualTracking = true
	return &Manager{
		cfg:        cfg,
		links:      map[string]string{"https://example.com/a": "link-uuid"},
		log:        log.New(io.Discard, "", 0),
		messengers: map[string]Messenger{},
	}
}

const linkBody = `U={{ UnsubscribeURL }}|M={{ ManageURL }}|V={{ MessageURL }}|O={{ OptinURL }}|A={{ ArchiveURL }}|R={{ RootURL }}|T={{ TrackLink "https://example.com/a" }}|P={{ TrackView }}`

func linkCamp(messenger string) *models.Campaign {
	return &models.Campaign{Base: models.Base{ID: 5}, UUID: "c-uuid", Name: "n", Messenger: messenger,
		Body: linkBody, ContentType: models.CampaignContentTypeHTML}
}

var linkSub = models.Subscriber{Base: models.Base{ID: 7}, UUID: "s-uuid", Email: "x@example.com"}

// render compiles c with the manager's funcs and returns the body and the List-Unsubscribe header.
func render(t *testing.T, m *Manager, c *models.Campaign) (string, string, error) {
	t.Helper()
	if err := c.CompileTemplate(m.TemplateFuncs(c)); err != nil {
		t.Fatal(err)
	}
	msg, err := m.NewCampaignMessage(c, linkSub)
	if err != nil {
		return "", "", err
	}
	return string(msg.Body()), m.campaignOut(msg).Headers.Get("List-Unsubscribe"), nil
}

// The pre-patch output for the instance root URL. Any change here changes RI mail.
const riGolden = `U=https://news.rangeinsiders.com/subscription/c-uuid/s-uuid|M=https://news.rangeinsiders.com/subscription/c-uuid/s-uuid?manage=true|V=https://news.rangeinsiders.com/campaign/c-uuid/s-uuid|O=https://news.rangeinsiders.com/subscription/optin/s-uuid?|A=https://news.rangeinsiders.com/archive|R=https://news.rangeinsiders.com|T=https://news.rangeinsiders.com/link/link-uuid/c-uuid/s-uuid|P=<img src="https://news.rangeinsiders.com/campaign/c-uuid/s-uuid/px.png" alt="" /><img src="https://news.rangeinsiders.com/campaign/c-uuid/s-uuid/px.png" alt="" />`

func TestLinkHostGoldenRootURL(t *testing.T) {
	m := linkTestManager(urlCfg(riRoot))
	body, hdr, err := render(t, m, linkCamp("email-ri"))
	if err != nil {
		t.Fatal(err)
	}
	if body != riGolden {
		t.Fatalf("body changed:\n got %s\nwant %s", body, riGolden)
	}
	if hdr != "<https://news.rangeinsiders.com/subscription/c-uuid/s-uuid>" {
		t.Fatalf("header: %s", hdr)
	}
}

const taRoot = "https://news.tacticaladvisor.net"

func hostFor(host string, err error) func(*models.Campaign) (string, error) {
	return func(*models.Campaign) (string, error) { return host, err }
}

func TestLinkHostBrandOverride(t *testing.T) {
	cfg := urlCfg(riRoot)
	cfg.LinkHostFor = hostFor(taRoot, nil)
	m := linkTestManager(cfg)
	body, hdr, err := render(t, m, linkCamp("email-ta"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(riGolden, riRoot, taRoot)
	if body != want {
		t.Fatalf("body:\n got %s\nwant %s", body, want)
	}
	if strings.Contains(body+hdr, "rangeinsiders") {
		t.Fatalf("RI host leaked: %s %s", body, hdr)
	}
	if hdr != "<"+taRoot+"/subscription/c-uuid/s-uuid>" {
		t.Fatalf("header: %s", hdr)
	}
}

func TestLinkHostEmptyResolverKeepsGolden(t *testing.T) {
	cfg := urlCfg(riRoot)
	cfg.LinkHostFor = hostFor("", nil)
	body, _, err := render(t, linkTestManager(cfg), linkCamp("email-ri"))
	if err != nil || body != riGolden {
		t.Fatalf("got %s, %v", body, err)
	}
}

func TestLinkHostResolverErrorFailsClosed(t *testing.T) {
	cfg := urlCfg(riRoot)
	cfg.LinkHostFor = hostFor("", fmt.Errorf("messenger mismatch"))
	m := linkTestManager(cfg)
	if _, _, err := render(t, m, linkCamp("email")); err == nil {
		t.Fatal("a resolver error must stop the message")
	}
}

// A campaign compiled once (a pipe) keeps its host when the brand changes later;
// the next compile picks up the new host.
func TestLinkHostFrozenPerCompile(t *testing.T) {
	host := taRoot
	cfg := urlCfg(riRoot)
	cfg.LinkHostFor = func(*models.Campaign) (string, error) { return host, nil }
	m := linkTestManager(cfg)
	c := linkCamp("email-ta")
	if err := c.CompileTemplate(m.TemplateFuncs(c)); err != nil {
		t.Fatal(err)
	}
	host = "https://other.example.net"
	msg, err := m.NewCampaignMessage(c, linkSub)
	if err != nil || !strings.Contains(string(msg.Body()), taRoot) || strings.Contains(string(msg.Body()), "other.example") {
		t.Fatalf("pipe must keep the old host: %s %v", msg.Body(), err)
	}
	c2 := linkCamp("email-ta")
	body, _, err := render(t, m, c2)
	if err != nil || !strings.Contains(body, "https://other.example.net/subscription/") {
		t.Fatalf("new compile must use the new host: %s %v", body, err)
	}
}

// The automation path (SendCampaignMessage) carries the brand host in the header.
func TestLinkHostSendCampaignMessage(t *testing.T) {
	cfg := urlCfg(riRoot)
	cfg.LinkHostFor = hostFor(taRoot, nil)
	m := linkTestManager(cfg)
	f := &fakeMsgr{name: "email-ta"}
	m.messengers["email-ta"] = f
	c := linkCamp("email-ta")
	if err := c.CompileTemplate(m.TemplateFuncs(c)); err != nil {
		t.Fatal(err)
	}
	msg, err := m.NewCampaignMessage(c, linkSub)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SendCampaignMessage(msg); err != nil {
		t.Fatal(err)
	}
	if got := f.last.Headers.Get("List-Unsubscribe"); got != "<"+taRoot+"/subscription/c-uuid/s-uuid>" {
		t.Fatalf("header: %s", got)
	}
	if strings.Contains(string(f.last.Body), "rangeinsiders") {
		t.Fatalf("body: %s", f.last.Body)
	}
}

// TemplateFuncs(nil) (template previews) needs no campaign and no resolver call.
func TestLinkHostNilCampaign(t *testing.T) {
	cfg := urlCfg(riRoot)
	cfg.LinkHostFor = func(*models.Campaign) (string, error) { t.Fatal("resolver called for nil campaign"); return "", nil }
	if f := linkTestManager(cfg).TemplateFuncs(nil); f["RootURL"].(func() string)() != riRoot {
		t.Fatal("root")
	}
}

// newPipe pauses a campaign whose link host cannot be resolved and sends nothing.
func TestLinkHostNewPipePauses(t *testing.T) {
	cfg := urlCfg(riRoot)
	cfg.LinkHostFor = hostFor("", fmt.Errorf("db down"))
	m := linkTestManager(cfg)
	st := &statusStore{}
	m.store = st
	m.messengers["email-ta"] = &fakeMsgr{name: "email-ta"}
	m.pipes = map[int]*pipe{}
	if _, err := m.newPipe(linkCamp("email-ta")); err == nil {
		t.Fatal("newPipe must fail")
	}
	if st.status != models.CampaignStatusPaused || len(m.pipes) != 0 {
		t.Fatalf("status %q, pipes %d", st.status, len(m.pipes))
	}
}

type statusStore struct {
	Store
	status string
}

func (s *statusStore) UpdateCampaignStatus(_ int, status string) error { s.status = status; return nil }
func (s *statusStore) GetAttachment(int) (models.Attachment, error)    { return models.Attachment{}, nil }
