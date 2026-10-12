package brands

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reSlug   = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)
	reDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
)

// Validate checks a brand from the admin API and replaces nil arrays with
// empty ones, because the columns are NOT NULL and a JSON body may omit them.
// from_domains entries must be bare lowercase domains: the send guard
// compares them with the exact domain of a from address.
func Validate(b *Brand) error {
	normalize(b)
	if !reSlug.MatchString(b.Slug) {
		return fmt.Errorf("invalid slug %q: must match %s", b.Slug, reSlug.String())
	}
	if b.Slug == SharedOwner {
		return fmt.Errorf("invalid slug %q: reserved for shared items", b.Slug)
	}
	if strings.TrimSpace(b.Name) == "" {
		return fmt.Errorf("invalid name: must not be empty")
	}
	for _, d := range b.FromDomains {
		if !reDomain.MatchString(d) {
			return fmt.Errorf("invalid from_domains entry %q: use a bare lowercase domain such as example.com", d)
		}
	}
	for _, t := range b.ListTags {
		n := NormalizeListTag(t)
		if n == "" {
			return fmt.Errorf("invalid list_tags entry %q: a tag must not be empty", t)
		}
		if n != t {
			return fmt.Errorf("list tag %q is stored as %q; use that", t, n)
		}
	}
	return nil
}

var reSpaces = regexp.MustCompile(`[\s]+`)

// NormalizeListTag is what core.normalizeTags stores for one list tag: it trims
// the tag and turns every whitespace run into "-". It does not change case.
// TestNormalizeListTagMatchesCore keeps it equal to the core function.
func NormalizeListTag(t string) string {
	return reSpaces.ReplaceAllString(strings.TrimSpace(t), "-")
}

// SharedOwner is the brand query value that creates a shared template or media file.
const SharedOwner = "shared"

func normalize(b *Brand) {
	if b.ListTags == nil {
		b.ListTags = []string{}
	}
	if b.Messengers == nil {
		b.Messengers = []string{}
	}
	if b.FromDomains == nil {
		b.FromDomains = []string{}
	}
}
