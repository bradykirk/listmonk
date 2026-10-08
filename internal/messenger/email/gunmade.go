package email

import (
	"errors"
	"fmt"
	"strings"
)

// gunmade fork: several businesses share one listmonk instance, each with its
// own SMTP account. Upstream's default `email` messenger round-robins across
// every enabled server, which would send one business's system and
// transactional mail through another business's account. The default can be
// pinned to one named server instead (LISTMONK_EMAIL_DEFAULT in cmd/).

// ErrNoServers is returned by Push when the messenger has no SMTP servers. The
// default `email` messenger is left empty, rather than falling back to another
// business's server, when the pinned server is missing or disabled.
var ErrNoServers = errors.New("e-mail messenger has no SMTP servers (check LISTMONK_EMAIL_DEFAULT and SMTP settings)")

// NormalizeName applies the same "email-" prefix that the settings handler
// stores, so `ri` and `email-ri` both match the server saved as `email-ri`.
func NormalizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || strings.HasPrefix(name, "email-") {
		return name
	}
	return "email-" + name
}

// NewMessengers returns the default `email` messenger followed by one
// standalone messenger per named server. Pass only enabled servers.
//
//   - Every named server is its own messenger, also when it is the only server
//     (upstream skips that case, so a campaign cannot select it).
//   - defaultName == "": `email` pools every server (upstream behavior).
//   - defaultName != "": `email` uses only the server with that name. If there
//     is none, `email` has no servers and every Push returns ErrNoServers. It
//     never falls back to another server.
//
// The returned warning is non-empty when `email` was left without servers.
func NewMessengers(servers []Server, defaultName string) ([]*Emailer, string, error) {
	var (
		named   []*Emailer
		def     = servers
		warning string
	)

	for _, s := range servers {
		if s.Name == "" {
			continue
		}
		m, err := New(s.Name, s)
		if err != nil {
			return nil, "", fmt.Errorf("error initializing e-mail messenger %s: %v", s.Name, err)
		}
		named = append(named, m)
	}

	if want := NormalizeName(defaultName); want != "" {
		def = nil
		for _, s := range servers {
			if NormalizeName(s.Name) == want {
				def = []Server{s}
				break
			}
		}
		if def == nil {
			warning = fmt.Sprintf("no enabled SMTP server named %q; the default `email` messenger has no "+
				"servers and all system and transactional e-mail will FAIL", want)
		}
	}

	m, err := New(MessengerName, def...)
	if err != nil {
		return nil, "", fmt.Errorf("error initializing e-mail messenger: %v", err)
	}

	return append([]*Emailer{m}, named...), warning, nil
}

// ServerNames returns the names of the servers in the messenger's default
// (round-robin) pool, in order.
func (e *Emailer) ServerNames() []string {
	out := make([]string, 0, len(e.pools[""]))
	for _, s := range e.pools[""] {
		out = append(out, s.Name)
	}
	return out
}
