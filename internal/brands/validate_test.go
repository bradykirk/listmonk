package brands

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := Brand{Slug: "ta", Name: "Tactical Advisor"}
	if err := Validate(&ok); err != nil {
		t.Fatalf("valid brand: %v", err)
	}
	// Omitted arrays become empty, never nil (NOT NULL columns).
	if ok.ListTags == nil || ok.Messengers == nil || ok.FromDomains == nil {
		t.Fatalf("nil arrays not normalized: %+v", ok)
	}

	good := Brand{Slug: "ri-2", Name: "x", FromDomains: []string{"rangeinsiders.com", "news.tacticaladvisor.net"}}
	if err := Validate(&good); err != nil {
		t.Fatalf("good domains: %v", err)
	}

	bad := []struct {
		b    Brand
		want string
	}{
		{Brand{Slug: "Bad", Name: "x"}, "slug"},
		{Brand{Slug: "a", Name: "x"}, "slug"},
		{Brand{Slug: "1ab", Name: "x"}, "slug"},
		{Brand{Slug: "shared", Name: "x"}, "reserved"},
		{Brand{Slug: "ab", Name: "  "}, "name"},
		{Brand{Slug: "ab", Name: "x", FromDomains: []string{"@example.com"}}, "@example.com"},
		{Brand{Slug: "ab", Name: "x", FromDomains: []string{"*.example.com"}}, "*.example.com"},
		{Brand{Slug: "ab", Name: "x", FromDomains: []string{"example.com."}}, "example.com."},
		{Brand{Slug: "ab", Name: "x", FromDomains: []string{"exa mple.com"}}, "exa mple.com"},
		{Brand{Slug: "ab", Name: "x", FromDomains: []string{"a@example.com"}}, "a@example.com"},
		{Brand{Slug: "ab", Name: "x", FromDomains: []string{"Example.com"}}, "Example.com"},
		{Brand{Slug: "ab", Name: "x", FromDomains: []string{""}}, "from_domains"},
	}
	for _, c := range bad {
		err := Validate(&c.b)
		if err == nil {
			t.Errorf("%+v: accepted", c.b)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: error %q does not name %q", c.b, err, c.want)
		}
	}
}

func TestVisible(t *testing.T) {
	ri, ta := "ri", "ta"
	owners := map[int]*string{1: &ri, 2: nil, 3: &ta}
	for id, want := range map[int]bool{1: true, 2: true, 3: false, 4: false} {
		if got := Visible(owners, "ri", id); got != want {
			t.Errorf("Visible(%d) = %v, want %v", id, got, want)
		}
	}
}
