package subimporter

import (
	"io"
	"log"
	"testing"

	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/models"
)

func testImporter(t *testing.T) *Importer {
	t.Helper()

	i, err := i18n.New([]byte(`{"_.code": "en", "_.name": "English", "subscribers.invalidEmail": "Invalid email."}`))
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}

	return New(Options{}, nil, i)
}

// gunmade fork: a subscriber without a name keeps no name. This used to be
// "John Smith", invented from the address.
func TestValidateFieldsKeepsMissingNameBlank(t *testing.T) {
	s, err := testImporter(t).ValidateFields(SubReq{Subscriber: models.Subscriber{Email: "John.Smith@Example.com"}})
	if err != nil {
		t.Fatalf("ValidateFields: %v", err)
	}
	if s.Name != "" || s.FirstName != "" || s.LastName != "" {
		t.Errorf("got %q / %q / %q, want all empty", s.FirstName, s.LastName, s.Name)
	}
	if s.Email != "john.smith@example.com" {
		t.Errorf("email = %q", s.Email)
	}
}

func TestValidateFieldsNames(t *testing.T) {
	cases := []struct {
		label                 string
		in                    models.Subscriber
		first, last, wantName string
	}{
		{"legacy name is split", models.Subscriber{Name: "Mary Jo Smith"}, "Mary", "Jo Smith", "Mary Jo Smith"},
		{"first/last win", models.Subscriber{Name: "Ignored", FirstName: "Ann", LastName: "Lee"}, "Ann", "Lee", "Ann Lee"},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			c.in.Email = "person@example.com"
			s, err := testImporter(t).ValidateFields(SubReq{Subscriber: c.in})
			if err != nil {
				t.Fatalf("ValidateFields: %v", err)
			}
			if s.FirstName != c.first || s.LastName != c.last || s.Name != c.wantName {
				t.Errorf("got %q / %q / %q, want %q / %q / %q", s.FirstName, s.LastName, s.Name, c.first, c.last, c.wantName)
			}
		})
	}
}

func TestCSVHeadersIncludeFirstAndLastName(t *testing.T) {
	s := &Session{log: log.New(io.Discard, "", 0)}

	got := s.mapCSVHeaders([]string{"email", "first_name", "last_name", "name", "attributes"}, csvHeaders)
	for _, h := range []string{"email", "first_name", "last_name", "name", "attributes"} {
		if _, ok := got[h]; !ok {
			t.Errorf("header %q not recognised", h)
		}
	}
}
