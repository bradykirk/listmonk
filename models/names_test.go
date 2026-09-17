package models

import (
	"bytes"
	"html/template"
	"testing"
)

// The exact outputs of the rule that subimporter.ValidateFields used to apply.
// Verified against the removed code on 2026-09-17. Postgres initcap differs on
// the first three rows, which is why the migration runs this rule in Go.
func TestFallbackName(t *testing.T) {
	cases := []struct{ email, want string }{
		{"john_doe@example.com", "John_doe"},
		{"o'brien@example.com", "O'brien"},
		{"a1b2@example.com", "A1b2"},
		{"j.doe@example.com", "J Doe"},
		{"info+news@example.com", "Info+News"},
		{"bkirk00@example.com", "Bkirk00"},
		{"ÉMILE@example.com", "Émile"},
		{"JOHN.SMITH@example.com", "John Smith"},
	}
	for _, c := range cases {
		if got := FallbackName(c.email); got != c.want {
			t.Errorf("FallbackName(%q) = %q, want %q", c.email, got, c.want)
		}
	}
}

func TestIsFallbackName(t *testing.T) {
	cases := []struct {
		name, email string
		want        bool
	}{
		{"Bkirkpatrick00", "bkirkpatrick00@example.com", true},
		{"John Smith", "john.smith@example.com", true},
		{"  John Smith ", "john.smith@example.com", true},
		{"John_doe", "john_doe@example.com", true},
		// initcap-style output was never produced by listmonk.
		{"John_Doe", "john_doe@example.com", false},
		// Public subscription form rule: the raw local part.
		{"pop.up", "pop.up@example.com", true},
		{"Johnny S.", "john.smith@example.com", false},
		{"Brady Kirkpatrick", "bkirk00@example.com", false},
		{"", "john.smith@example.com", false},
	}
	for _, c := range cases {
		if got := IsFallbackName(c.name, c.email); got != c.want {
			t.Errorf("IsFallbackName(%q, %q) = %v, want %v", c.name, c.email, got, c.want)
		}
	}
}

func TestSplitName(t *testing.T) {
	cases := []struct{ name, first, last string }{
		{"", "", ""},
		{"   ", "", ""},
		{"Cher", "Cher", ""},
		{"Brady Kirkpatrick", "Brady", "Kirkpatrick"},
		{"Mary Jo Van Der Berg", "Mary", "Jo Van Der Berg"},
		{"  Pat   O'Brien  ", "Pat", "O'Brien"},
	}
	for _, c := range cases {
		first, last := SplitName(c.name)
		if first != c.first || last != c.last {
			t.Errorf("SplitName(%q) = %q, %q; want %q, %q", c.name, first, last, c.first, c.last)
		}
	}
}

func TestJoinName(t *testing.T) {
	cases := []struct{ first, last, want string }{
		{"", "", ""},
		{"Cher", "", "Cher"},
		{"", "Doe", "Doe"},
		{"Mary", "Jo Smith", "Mary Jo Smith"},
	}
	for _, c := range cases {
		if got := JoinName(c.first, c.last); got != c.want {
			t.Errorf("JoinName(%q, %q) = %q, want %q", c.first, c.last, got, c.want)
		}
	}
}

func TestResolveSubscriberNames(t *testing.T) {
	stored := Subscriber{FirstName: "Mary", LastName: "Smith", Name: "Mary Smith"}

	cases := []struct {
		label string
		in    Subscriber
		prev  *Subscriber
		want  Subscriber
	}{
		{"create: legacy name only is split", Subscriber{Name: " Mary Jo Smith "}, nil,
			Subscriber{FirstName: "Mary", LastName: "Jo Smith", Name: "Mary Jo Smith"}},
		{"create: first/last win over a stale name", Subscriber{Name: "Ignored", FirstName: " Ann ", LastName: "Lee"}, nil,
			Subscriber{FirstName: "Ann", LastName: "Lee", Name: "Ann Lee"}},
		{"create: no name stays blank", Subscriber{}, nil, Subscriber{}},
		{"patch: legacy name changed", Subscriber{FirstName: "Mary", LastName: "Smith", Name: "Jane Doe"}, &stored,
			Subscriber{FirstName: "Jane", LastName: "Doe", Name: "Jane Doe"}},
		{"patch: first name changed", Subscriber{FirstName: "Janet", LastName: "Smith", Name: "Mary Smith"}, &stored,
			Subscriber{FirstName: "Janet", LastName: "Smith", Name: "Janet Smith"}},
		{"patch: nothing changed", stored, &stored, stored},
		{"patch: name cleared", Subscriber{FirstName: "Mary", LastName: "Smith", Name: ""}, &stored, Subscriber{}},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			got := c.in
			ResolveSubscriberNames(&got, c.prev)
			if got.FirstName != c.want.FirstName || got.LastName != c.want.LastName || got.Name != c.want.Name {
				t.Errorf("got %q / %q / %q, want %q / %q / %q",
					got.FirstName, got.LastName, got.Name, c.want.FirstName, c.want.LastName, c.want.Name)
			}
		})
	}
}

// Templates written as {{ .Subscriber.FirstName }} against the old methods must
// keep working against the fields, including the blank-name greeting idiom.
func TestSubscriberNameFieldsInTemplates(t *testing.T) {
	tpl := template.Must(template.New("t").Parse(
		`Hello{{ if .Subscriber.FirstName }} {{ .Subscriber.FirstName }}{{ end }}, {{ .Subscriber.LastName }}`))

	cases := []struct {
		sub  Subscriber
		want string
	}{
		{Subscriber{FirstName: "Mary", LastName: "Smith"}, "Hello Mary, Smith"},
		{Subscriber{}, "Hello, "},
	}
	for _, c := range cases {
		var b bytes.Buffer
		if err := tpl.Execute(&b, map[string]any{"Subscriber": c.sub}); err != nil {
			t.Fatalf("execute: %v", err)
		}
		if b.String() != c.want {
			t.Errorf("got %q, want %q", b.String(), c.want)
		}
	}
}
