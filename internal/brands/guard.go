package brands

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// GuardInput is what the send guard checks for one campaign.
type GuardInput struct {
	ListIDs   []int
	Messenger string
	FromEmail string
}

// Check returns nil when the lists, messenger and from domain all belong to
// one brand. The error text names the first failed rule.
func Check(in GuardInput, listTags map[int][]string, brands []Brand) error {
	slug, reason := CampaignBrand(in.ListIDs, listTags, brands)
	if slug == "" {
		return errors.New(reason)
	}
	return CheckSender(slug, in.Messenger, in.FromEmail, brands)
}

// CheckSender checks the messenger and from domain against one brand.
func CheckSender(slug, messenger, fromEmail string, brands []Brand) error {
	var b *Brand
	for i := range brands {
		if brands[i].Slug == slug {
			b = &brands[i]
		}
	}
	if b == nil {
		return fmt.Errorf("Brand %s does not exist.", slug)
	}
	if !containsStr(b.Messengers, messenger) {
		return fmt.Errorf("Messenger %s is not allowed for brand %s.", messenger, slug)
	}
	addr, err := mail.ParseAddress(fromEmail)
	if err != nil {
		return errors.New("The from address is not a valid email address.")
	}
	if !DomainAllowed(addr.Address, b.FromDomains) {
		return fmt.Errorf("From address domain %s is not allowed for brand %s.", domainOf(addr.Address), slug)
	}
	return nil
}

// DomainAllowed reports whether the email domain equals an allowed domain or
// is a subdomain of one. The match is case-insensitive.
func DomainAllowed(email string, domains []string) bool {
	d := domainOf(email)
	for _, allowed := range domains {
		a := strings.ToLower(strings.TrimSpace(allowed))
		if a != "" && (d == a || strings.HasSuffix(d, "."+a)) {
			return true
		}
	}
	return false
}

func domainOf(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
