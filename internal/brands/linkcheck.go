package brands

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// LinkCheckPath is the public route that proves a link host reaches this
// instance. A brand save sends a lookup token there and expects the paired
// response token, which only this process knows (plan D4).
const LinkCheckPath = "/ext/link-host-check/"

const linkCheckTTL = 30 * time.Second

type linkPair struct {
	resp string
	exp  time.Time
}

// LinkChecks holds the live token pairs of this process.
type LinkChecks struct {
	mu  sync.Mutex
	m   map[string]linkPair
	now func() time.Time
}

func NewLinkChecks() *LinkChecks {
	return &LinkChecks{m: map[string]linkPair{}, now: time.Now}
}

func token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (lc *LinkChecks) begin() (lookup, resp string) {
	lookup, resp = token(), token()
	lc.mu.Lock()
	defer lc.mu.Unlock()
	now := lc.now()
	for k, p := range lc.m {
		if now.After(p.exp) {
			delete(lc.m, k)
		}
	}
	lc.m[lookup] = linkPair{resp: resp, exp: now.Add(linkCheckTTL)}
	return lookup, resp
}

func (lc *LinkChecks) end(lookup string) {
	lc.mu.Lock()
	delete(lc.m, lookup)
	lc.mu.Unlock()
}

// Answer returns the response token for a live lookup token.
func (lc *LinkChecks) Answer(lookup string) (string, bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	p, ok := lc.m[lookup]
	if !ok || lc.now().After(p.exp) {
		return "", false
	}
	return p.resp, true
}

// LinkCheckClient returns a client that refuses redirects and times out after
// 5 s. base supplies the transport (tests); nil uses the default transport.
func LinkCheckClient(base *http.Client) *http.Client {
	c := &http.Client{Timeout: 5 * time.Second}
	if base != nil {
		c.Transport = base.Transport
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

// Verify proves that host (https://name, normalized) is served by this process.
func (lc *LinkChecks) Verify(host string, client *http.Client) error {
	lookup, want := lc.begin()
	defer lc.end(lookup)
	res, err := client.Get(host + LinkCheckPath + lookup)
	if err != nil {
		return fmt.Errorf("link host %s is not reachable over https: %w", host, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("link host %s does not reach this Listmonk (HTTP %d)", host, res.StatusCode)
	}
	got, err := io.ReadAll(io.LimitReader(res.Body, 256))
	if err != nil {
		return fmt.Errorf("link host %s: %w", host, err)
	}
	if subtle.ConstantTimeCompare(got, []byte(want)) != 1 {
		return errors.New("link host " + host + " answers, but not from this Listmonk instance")
	}
	return nil
}

// BrandRequest is a brand body where link_host may be absent (keep the stored value).
type BrandRequest struct {
	Brand
	LinkHost *string `json:"link_host"`
}

// ApplyLinkHost sets b.LinkHost from the request, or keeps prev when the field
// is absent. It normalizes the value and, for a new non-empty host, calls
// verify (Verify through the public route) before accepting it.
func ApplyLinkHost(b *Brand, req *string, prev string, verify func(host string) error) error {
	b.LinkHost = prev
	if req == nil {
		return nil
	}
	host, err := NormalizeLinkHost(*req)
	if err != nil {
		return err
	}
	if host != "" && host != prev {
		if err := verify(host); err != nil {
			return err
		}
	}
	b.LinkHost = host
	return nil
}
