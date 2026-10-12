package brands

import "testing"

func guardBrands() []Brand {
	return []Brand{
		{Slug: "ri", ListTags: []string{"ri", "ri-cold"}, Messengers: []string{"email-ri"}, FromDomains: []string{"rangeinsiders.com"}},
		{Slug: "ta", ListTags: []string{"ta"}, Messengers: []string{"email-ta"}, FromDomains: []string{"news.tacticaladvisor.net"}},
		{Slug: "ct", ListTags: []string{"ct"}, Messengers: []string{"email-ct"}, FromDomains: []string{}},
	}
}

var guardTags = map[int][]string{3: {"ri"}, 6: {"ri-cold"}, 17: {"ta"}, 30: {"ct"}, 40: {"misc"}}

func TestGuard(t *testing.T) {
	ok := GuardInput{ListIDs: []int{3, 6}, Messenger: "email-ri", FromEmail: "RangeInsiders <news@news.rangeinsiders.com>"}
	if err := Check(ok, guardTags, guardBrands()); err != nil {
		t.Fatal(err)
	}

	bad := []struct {
		name string
		in   GuardInput
		want string
	}{
		{"mixed", GuardInput{[]int{3, 17}, "email-ri", "a@rangeinsiders.com"}, "List 3 belongs to ri, but list 17 belongs to ta."},
		{"no lists", GuardInput{nil, "email-ri", "a@rangeinsiders.com"}, "The campaign has no lists."},
		{"unassigned", GuardInput{[]int{40}, "email-ri", "a@rangeinsiders.com"}, "List 40 belongs to no brand."},
		{"messenger", GuardInput{[]int{17}, "email-ri", "a@news.tacticaladvisor.net"}, "Messenger email-ri is not allowed for brand ta."},
		{"domain", GuardInput{[]int{3}, "email-ri", "a@news.tacticaladvisor.net"}, "From address domain news.tacticaladvisor.net is not allowed for brand ri."},
		{"lookalike", GuardInput{[]int{3}, "email-ri", "a@evilrangeinsiders.com"}, "From address domain evilrangeinsiders.com is not allowed for brand ri."},
		{"suffix trick", GuardInput{[]int{3}, "email-ri", "a@rangeinsiders.com.evil.io"}, "From address domain rangeinsiders.com.evil.io is not allowed for brand ri."},
		{"no domain set", GuardInput{[]int{30}, "email-ct", "a@ct.example"}, "From address domain ct.example is not allowed for brand ct."},
		{"unparsable from", GuardInput{[]int{3}, "email-ri", "not an address"}, "The from address is not a valid email address."},
	}
	for _, c := range bad {
		err := Check(c.in, guardTags, guardBrands())
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: got %v want %q", c.name, err, c.want)
		}
	}
}

func TestDomainAllowedCase(t *testing.T) {
	if !DomainAllowed("A@News.RangeInsiders.COM", []string{"rangeinsiders.com"}) {
		t.Fatal("case")
	}
}

func TestCheckSender(t *testing.T) {
	if err := CheckSender("ri", "email-ri", "news@rangeinsiders.com", guardBrands()); err != nil {
		t.Fatal(err)
	}
	if err := CheckSender("zz", "email-ri", "news@rangeinsiders.com", guardBrands()); err == nil {
		t.Fatal("unknown brand must fail")
	}
}
