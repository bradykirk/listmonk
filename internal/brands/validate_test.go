package brands

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
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

func TestValidateListTags(t *testing.T) {
	for _, tags := range [][]string{{"ri", "ri-cold"}, {"ta"}, {"ct"}, {"Mixed-Case"}} {
		b := Brand{Slug: "ab", Name: "x", ListTags: tags}
		if err := Validate(&b); err != nil {
			t.Errorf("%v: %v", tags, err)
		}
	}
	b := Brand{Slug: "ab", Name: "x", ListTags: []string{"new brand"}}
	err := Validate(&b)
	if err == nil || err.Error() != `list tag "new brand" is stored as "new-brand"; use that` {
		t.Fatalf("got %v", err)
	}
	for _, bad := range []string{" ri", "ri ", "a  b", "a\tb", ""} {
		b := Brand{Slug: "ab", Name: "x", ListTags: []string{bad}}
		if Validate(&b) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// The brand tag rule must equal what list storage does (core.normalizeTags).
// CreateList inserts the normalized tags, then reloads the list with a query this
// test setup does not prepare, so the reload panics and is recovered here. The
// stored tags are read back from the database.
func TestNormalizeListTagMatchesCore(t *testing.T) {
	db := testDB(t)
	q := campaignTestQueries(t, db)
	c := core.New(&core.Opt{Queries: q, DB: db, Log: log.New(io.Discard, "", 0)}, &core.Hooks{})
	for _, in := range []string{"ri", "ri-cold", "new brand", " x ", "a  b", "a\tb", "Mixed Case", "a-b c"} {
		func() {
			defer func() { _ = recover() }()
			c.CreateList(models.List{Name: "n", Tags: []string{in}})
		}()
		var tags pq.StringArray
		if err := db.Get(&tags, `SELECT tags FROM lists ORDER BY id DESC LIMIT 1`); err != nil {
			t.Fatal(err)
		}
		if got := NormalizeListTag(in); len(tags) != 1 || tags[0] != got {
			t.Errorf("%q: core stored %v, NormalizeListTag %q", in, tags, got)
		}
	}
}
