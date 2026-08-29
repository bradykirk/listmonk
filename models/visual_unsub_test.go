package models

import (
	"bytes"
	"strings"
	"testing"
)

// renderVisual compiles and renders a visual campaign for the tests below.
func renderVisual(t *testing.T, c *Campaign) string {
	t.Helper()
	if err := c.CompileTemplate(stubFuncs()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	var b bytes.Buffer
	if err := c.Tpl.ExecuteTemplate(&b, BaseTpl, nil); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	return b.String()
}

// The case that shipped: a visual campaign with no unsubscribe link of its own,
// attached to a stock template that carries one. Visual campaigns never render
// their template, so the template must not satisfy the unsubscribe check and
// the footer must be injected into the visual document itself.
func TestCompileTemplateVisualWithTemplateAttached(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeVisual,
		TemplateBody: `<html><body>{{ template "content" . }}<a href="{{ UnsubscribeURL }}">Unsubscribe</a></body></html>`,
		Body:         `<html><body><p>Newsletter</p></body></html>`,
	}
	out := renderVisual(t, c)

	if !strings.Contains(out, "https://lists.gunmade.com/unsub") {
		t.Errorf("rendered visual campaign has no unsubscribe link:\n%s", out)
	}
	// The footer must sit inside the visual document, not after </html>.
	if strings.Index(out, "https://lists.gunmade.com/unsub") > strings.Index(out, "</body>") {
		t.Errorf("footer placed outside the visual document body:\n%s", out)
	}
	// Exactly one footer.
	if n := strings.Count(out, "https://lists.gunmade.com/unsub"); n != 1 {
		t.Errorf("expected exactly 1 unsubscribe link, got %d:\n%s", n, out)
	}
}

// A visual campaign that carries its own link must not get a second footer.
func TestCompileTemplateVisualWithOwnLink(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeVisual,
		TemplateBody: `<html><body>{{ template "content" . }}</body></html>`,
		Body:         `<html><body><p>Newsletter</p><a href="{{ UnsubscribeURL }}">Unsubscribe</a></body></html>`,
	}
	out := renderVisual(t, c)

	if n := strings.Count(out, "https://lists.gunmade.com/unsub"); n != 1 {
		t.Errorf("expected exactly 1 unsubscribe link, got %d:\n%s", n, out)
	}
}
