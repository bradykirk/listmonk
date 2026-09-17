package models

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Subscriber name rules (gunmade fork).
//
// Subscribers store first_name and last_name. subscribers.name is a Postgres
// generated column, btrim(first_name || ' ' || last_name), so every existing
// read of name keeps working and any write to it fails loudly.
//
// These rules are shared by the fork migration, the importer, the HTTP
// handlers and scripts/fork/namecheck, so each one exists exactly once.

// FallbackName reproduces the name that the importer and the admin API used to
// invent for a subscriber created without one: the e-mail's local part, dots
// turned into spaces, each word title-cased. It must stay byte-identical to
// that removed rule, or the migration stops recognising the names it made.
func FallbackName(email string) string {
	name := strings.ToLower(strings.Split(email, "@")[0])

	parts := strings.Fields(strings.ReplaceAll(name, ".", " "))
	for n, p := range parts {
		parts[n] = cases.Title(language.Und).String(p)
	}

	return strings.Join(parts, " ")
}

// IsFallbackName reports whether name was invented by listmonk from email
// rather than supplied by a person. Two rules invented names: FallbackName
// (importer and admin API) and the raw local part (public subscription form).
func IsFallbackName(name, email string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}

	return name == FallbackName(email) || name == strings.Split(email, "@")[0]
}

// SplitName splits a full name into the first word and the remaining words.
func SplitName(name string) (first, last string) {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "", ""
	}

	return parts[0], strings.Join(parts[1:], " ")
}

// JoinName mirrors the subscribers.name generated column.
func JoinName(first, last string) string {
	return strings.TrimSpace(first + " " + last)
}

// ResolveSubscriberNames trims the name fields of s and derives FirstName and
// LastName from the legacy single Name when that is what the caller sent. Name
// is then rewritten to match the generated column.
//
// prev is the stored subscriber when the request was pre-filled from the
// database (PATCH), and nil otherwise (create, PUT, public forms):
//   - prev == nil: Name is split only when FirstName and LastName are both empty.
//   - prev != nil: Name is split only when Name changed and neither FirstName
//     nor LastName did.
func ResolveSubscriberNames(s *Subscriber, prev *Subscriber) {
	s.Name = strings.TrimSpace(s.Name)
	s.FirstName = strings.TrimSpace(s.FirstName)
	s.LastName = strings.TrimSpace(s.LastName)

	legacy := s.FirstName == "" && s.LastName == "" && s.Name != ""
	if prev != nil {
		legacy = s.Name != prev.Name && s.FirstName == prev.FirstName && s.LastName == prev.LastName
	}
	if legacy {
		s.FirstName, s.LastName = SplitName(s.Name)
	}

	s.Name = JoinName(s.FirstName, s.LastName)
}
