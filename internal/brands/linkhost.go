package brands

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/knadh/listmonk/models"
)

// Per-brand link host. A brand with a link_host gets every Listmonk URL in its
// campaign emails on that host instead of the instance root URL. Plan:
// docs/superpowers/plans/2026-11-10-brand-link-host.md.

var hostLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// NormalizeLinkHost returns "https://<lowercase host>" or "" for an empty value.
// It refuses anything else: another scheme, a path, a port, a query, user info,
// an IP address, or a host without a dot.
func NormalizeLinkHost(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Port() != "" ||
		u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("link host must look like https://news.example.com (no path, port or query)")
	}
	host := strings.ToLower(u.Hostname())
	if net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return "", fmt.Errorf("link host must be a domain name, not %q", host)
	}
	for _, l := range strings.Split(host, ".") {
		if !hostLabel.MatchString(l) {
			return "", fmt.Errorf("link host %q is not a valid domain name", host)
		}
	}
	return "https://" + host, nil
}

// LinkResolver returns the function the campaign manager calls once per
// campaign compile. It reads the brands fresh (no cache), takes the brand from
// the campaign's lists by campaign ID (Campaign.Lists is empty on the send
// path), and returns that brand's link host, "" for the root URL, or an error.
// It fails closed: when the brand has a link host and the campaign's messenger
// is not one of the brand's messengers, or a lookup fails, it returns an error
// so the campaign is not sent with the wrong host.
func (s *Service) LinkResolver(campaignLists func(id int) ([]int, error)) func(*models.Campaign) (string, error) {
	return func(c *models.Campaign) (string, error) {
		ids, err := campaignLists(c.ID)
		if err != nil {
			return "", fmt.Errorf("link host: loading lists of campaign %d: %w", c.ID, err)
		}
		b, t, err := s.loadAll()
		if err != nil {
			return "", fmt.Errorf("link host: loading brands: %w", err)
		}
		// With no link host on any brand there is nothing to resolve, so nothing
		// can fail (old campaigns with deleted or unbranded lists keep working).
		if !slices.ContainsFunc(b, func(x Brand) bool { return x.LinkHost != "" }) {
			return "", nil
		}
		// A deleted list (id 0 or gone) has no brand; the send guard handles it for
		// sends. Views of old campaigns use the lists that still exist.
		live := make([]int, 0, len(ids))
		for _, id := range ids {
			if _, ok := t[id]; ok && id > 0 {
				live = append(live, id)
			}
		}
		unbranded := true
		for _, id := range live {
			if lb := BrandOfList(t[id], b); lb.Slug != "" || lb.Ambiguous {
				unbranded = false
				break
			}
		}
		if unbranded {
			return "", nil
		}
		slug, reason := CampaignBrand(live, t, b)
		if slug == "" {
			return "", fmt.Errorf("link host: campaign %d: %s", c.ID, reason)
		}
		i := slices.IndexFunc(b, func(x Brand) bool { return x.Slug == slug })
		if i < 0 || b[i].LinkHost == "" {
			return "", nil
		}
		if !slices.Contains(b[i].Messengers, c.Messenger) {
			return "", fmt.Errorf("link host: campaign %d uses messenger %s, which brand %s does not allow", c.ID, c.Messenger, slug)
		}
		return b[i].LinkHost, nil
	}
}
