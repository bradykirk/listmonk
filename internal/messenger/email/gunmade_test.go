package email

import (
	"errors"
	"slices"
	"testing"

	"github.com/knadh/listmonk/models"
)

// srv returns an SMTP server pointing at a closed local port. New does not
// dial, so these tests never touch the network.
func srv(name string) Server {
	var s Server
	s.Name = name
	s.Host = "127.0.0.1"
	s.Port = 1
	s.TLSType = "none"
	s.MaxConns = 1
	return s
}

func names(ms []*Emailer) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name()
	}
	return out
}

func TestOneNamedServerIsRegistered(t *testing.T) {
	ms, warn, err := NewMessengers([]Server{srv("email-ri")}, "")
	if err != nil || warn != "" {
		t.Fatalf("err=%v warn=%q", err, warn)
	}
	if got := names(ms); !slices.Equal(got, []string{"email", "email-ri"}) {
		t.Fatalf("messengers = %v", got)
	}
}

func TestTwoNamedServersUnsetEnvKeepsUpstreamPool(t *testing.T) {
	ms, warn, err := NewMessengers([]Server{srv("email-ri"), srv("email-ta")}, "")
	if err != nil || warn != "" {
		t.Fatalf("err=%v warn=%q", err, warn)
	}
	if got := names(ms); !slices.Equal(got, []string{"email", "email-ri", "email-ta"}) {
		t.Fatalf("messengers = %v", got)
	}
	if got := ms[0].ServerNames(); !slices.Equal(got, []string{"email-ri", "email-ta"}) {
		t.Fatalf("email pool = %v, want both servers (upstream behavior)", got)
	}
}

func TestDefaultResolvesToNamedServerOnly(t *testing.T) {
	for _, env := range []string{"ri", "email-ri", " RI "} {
		// Order must not matter: the creator server comes first here.
		ms, warn, err := NewMessengers([]Server{srv("email-ta"), srv("email-ri")}, env)
		if err != nil || warn != "" {
			t.Fatalf("env %q: err=%v warn=%q", env, err, warn)
		}
		if got := ms[0].ServerNames(); !slices.Equal(got, []string{"email-ri"}) {
			t.Fatalf("env %q: email pool = %v, want only email-ri", env, got)
		}
		if got := names(ms); !slices.Equal(got, []string{"email", "email-ta", "email-ri"}) {
			t.Fatalf("env %q: messengers = %v", env, got)
		}
	}
}

// The RI server is disabled (so initSMTPMessengers drops it) while the creator
// server stays enabled. `email` must not fall back to the creator server.
func TestDefaultServerDisabledNeverFallsBack(t *testing.T) {
	ms, warn, err := NewMessengers([]Server{srv("email-ta")}, "ri")
	if err != nil {
		t.Fatal(err)
	}
	if warn == "" {
		t.Fatal("expected a warning when the default server is missing")
	}
	if got := names(ms); !slices.Equal(got, []string{"email", "email-ta"}) {
		t.Fatalf("messengers = %v", got)
	}
	if got := ms[0].ServerNames(); len(got) != 0 {
		t.Fatalf("email pool = %v, want empty", got)
	}
	err = ms[0].Push(models.Message{From: "a@b.c", To: []string{"x@y.z"}})
	if !errors.Is(err, ErrNoServers) {
		t.Fatalf("Push on empty email = %v, want ErrNoServers", err)
	}
}

func TestUnnamedServerOnlyInDefaultPool(t *testing.T) {
	ms, _, err := NewMessengers([]Server{srv("")}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(ms); !slices.Equal(got, []string{"email"}) {
		t.Fatalf("messengers = %v", got)
	}
	// With the default pinned, an unnamed server can never back `email`.
	ms, warn, _ := NewMessengers([]Server{srv("")}, "ri")
	if warn == "" || len(ms[0].ServerNames()) != 0 {
		t.Fatalf("unnamed server must not back a pinned default (warn=%q)", warn)
	}
}

func TestNormalizeName(t *testing.T) {
	for in, want := range map[string]string{"": "", "ri": "email-ri", "email-ri": "email-ri", " TA ": "email-ta"} {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}
