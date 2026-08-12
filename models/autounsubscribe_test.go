package models

import (
	"bytes"
	"strings"
	"testing"

	null "gopkg.in/volatiletech/null.v6"
)

func TestHasUnsubscribe(t *testing.T) {
	cases := []struct {
		name    string
		sources []string
		want    bool
	}{
		{"plain unsubscribe url", []string{`<a href="{{ UnsubscribeURL }}">out</a>`}, true},
		{"no spaces", []string{`{{UnsubscribeURL}}`}, true},
		{"manage url counts", []string{`<a href="{{ ManageURL }}">preferences</a>`}, true},
		{"found in the second source", []string{`<p>nothing</p>`, `{{ UnsubscribeURL }}`}, true},
		{"absent", []string{`<p>hello</p>`, `<p>world</p>`}, false},
		{"empty", []string{}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasUnsubscribe(c.sources...); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// The case that caused this: a hand-written template with no way out.
func TestCompileTemplateAddsMissingUnsubscribe(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeHTML,
		TemplateBody: `<html><body><div>{{ template "content" . }}</div></body></html>`,
		Body:         `<p>TX22 Competition just hit $519.99</p>`,
	}

	if err := c.CompileTemplate(stubFuncs()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	var b bytes.Buffer
	if err := c.Tpl.ExecuteTemplate(&b, BaseTpl, nil); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	out := b.String()

	if !strings.Contains(out, "https://lists.gunmade.com/unsub") {
		t.Errorf("unsubscribe link missing:\n%s", out)
	}
	if !strings.Contains(out, "Unsubscribe") {
		t.Errorf("unsubscribe wording missing:\n%s", out)
	}
	// It must land inside the document, not after </body>.
	if strings.Index(out, "unsub") > strings.Index(out, "</body>") {
		t.Errorf("footer placed outside the document body:\n%s", out)
	}
}

// A template that already has a link must be left exactly as it is.
func TestCompileTemplateDoesNotDuplicateUnsubscribe(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeHTML,
		TemplateBody: `<html><body>{{ template "content" . }}<a href="{{ UnsubscribeURL }}">Leave</a></body></html>`,
		Body:         `<p>hello</p>`,
	}

	if err := c.CompileTemplate(stubFuncs()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	var b bytes.Buffer
	if err := c.Tpl.ExecuteTemplate(&b, BaseTpl, nil); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if n := strings.Count(b.String(), "lists.gunmade.com/unsub"); n != 1 {
		t.Errorf("expected exactly 1 unsubscribe link, got %d:\n%s", n, b.String())
	}
}

// The link may live in the campaign content rather than the template.
func TestCompileTemplateHonoursUnsubscribeInBody(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeHTML,
		TemplateBody: `<html><body>{{ template "content" . }}</body></html>`,
		Body:         `<p>hi</p><a href="{{ UnsubscribeURL }}">Leave</a>`,
	}

	if err := c.CompileTemplate(stubFuncs()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	var b bytes.Buffer
	if err := c.Tpl.ExecuteTemplate(&b, BaseTpl, nil); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if n := strings.Count(b.String(), "lists.gunmade.com/unsub"); n != 1 {
		t.Errorf("expected exactly 1 unsubscribe link, got %d:\n%s", n, b.String())
	}
}

// A plain text campaign gets a text footer, never HTML.
func TestCompileTemplatePlainTextUnsubscribe(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypePlain,
		TemplateBody: `{{ template "content" . }}`,
		Body:         `Read more at https://gunmade.com/tx22`,
	}

	if err := c.CompileTemplate(stubFuncs()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	var b bytes.Buffer
	if err := c.Tpl.ExecuteTemplate(&b, BaseTpl, nil); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	out := b.String()

	if !strings.Contains(out, "https://lists.gunmade.com/unsub") {
		t.Errorf("unsubscribe missing from plain text:\n%s", out)
	}
	if strings.Contains(out, "<div") || strings.Contains(out, "<a ") {
		t.Errorf("html leaked into a plain text campaign:\n%s", out)
	}
}

// A text-only reader sees the alt body and nothing else.
func TestCompileTemplateAltBodyUnsubscribe(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeHTML,
		TemplateBody: `<html><body>{{ template "content" . }}</body></html>`,
		Body:         `<p>hi</p>`,
		AltBody:      null.StringFrom("TX22 competition just hit $519.99"),
	}

	if err := c.CompileTemplate(stubFuncs()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	if c.AltBodyTpl == nil {
		t.Fatal("alt body was not compiled after the footer was added")
	}

	var b bytes.Buffer
	if err := c.AltBodyTpl.Execute(&b, nil); err != nil {
		t.Fatalf("alt body execute failed: %v", err)
	}

	if !strings.Contains(b.String(), "https://lists.gunmade.com/unsub") {
		t.Errorf("unsubscribe missing from alt body:\n%s", b.String())
	}
}

// Compiling twice must not stack two footers onto the alt body.
func TestCompileTemplateAltBodyIsIdempotent(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeHTML,
		TemplateBody: `<html><body>{{ template "content" . }}</body></html>`,
		Body:         `<p>hi</p>`,
		AltBody:      null.StringFrom("hello"),
	}

	for i := 0; i < 3; i++ {
		if err := c.CompileTemplate(stubFuncs()); err != nil {
			t.Fatalf("compile %d failed: %v", i, err)
		}
	}

	if n := strings.Count(c.AltBody.String, "UnsubscribeURL"); n != 1 {
		t.Errorf("expected 1 footer after 3 compiles, got %d: %s", n, c.AltBody.String)
	}
}
