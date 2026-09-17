package models

import (
	"bytes"
	"strings"
	"testing"
)

const preheaderMarker = `mso-hide:all;">`

func TestAutoPreheaderHTML(t *testing.T) {
	cases := []struct {
		label, in, wantPrefix string
	}{
		{"after body with attributes", `<html><BODY class="x" style="y"><p>Hi</p></body></html>`, `<html><BODY class="x" style="y"><div style=`},
		{"no body tag prepends", `<p>Hi</p>`, `<div style=`},
		{"tbody is not body", `<table><tbody><tr><td>x</td></tr></tbody></table>`, `<div style=`},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			out := autoPreheaderHTML(c.in)
			if !strings.HasPrefix(out, c.wantPrefix) {
				t.Errorf("got %q, want prefix %q", out, c.wantPrefix)
			}
			if n := strings.Count(out, preheaderMarker); n != 1 {
				t.Errorf("preheader blocks = %d, want 1", n)
			}
		})
	}
}

func TestChoosePreheaderTarget(t *testing.T) {
	const (
		docBody  = `<html><body><p>x</p></body></html>`
		fragment = `<p>x</p>`
		include  = `{{ template "content" . }}`
	)
	cases := []struct {
		label string
		c     Campaign
		base  string
		want  preheaderTarget
	}{
		{"empty preview text", Campaign{ContentType: CampaignContentTypeHTML, Body: fragment}, docBody, preheaderNone},
		{"whitespace preview text", Campaign{PreviewText: "  ", ContentType: CampaignContentTypeHTML, Body: fragment}, docBody, preheaderNone},
		{"plain text campaign", Campaign{PreviewText: "p", ContentType: CampaignContentTypePlain, Body: fragment}, docBody, preheaderNone},
		{"template has body", Campaign{PreviewText: "p", ContentType: CampaignContentTypeRichtext, Body: fragment}, docBody, preheaderBase},
		{"content has body, base does not", Campaign{PreviewText: "p", ContentType: CampaignContentTypeHTML, Body: docBody}, include, preheaderContent},
		{"visual document", Campaign{PreviewText: "p", ContentType: CampaignContentTypeVisual, Body: docBody}, include, preheaderContent},
		{"neither has body", Campaign{PreviewText: "p", ContentType: CampaignContentTypeMarkdown, Body: "# hi"}, include, preheaderBase},
		{"author placed it in the template", Campaign{PreviewText: "p", ContentType: CampaignContentTypeRichtext, Body: fragment},
			`<body>{{ .Campaign.PreviewText }}` + include + `</body>`, preheaderNone},
		{"author placed it in the content", Campaign{PreviewText: "p", ContentType: CampaignContentTypeVisual,
			Body: `<body><span>{{ .Campaign.PreviewText }}</span></body>`}, include, preheaderNone},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			if got := choosePreheaderTarget(&c.c, c.base); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// renderWithCampaign compiles c and renders it with the data shape the manager
// uses, so {{ .Campaign.PreviewText }} resolves.
func renderWithCampaign(t *testing.T, c *Campaign) string {
	t.Helper()
	if err := c.CompileTemplate(stubFuncs()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	var b bytes.Buffer
	if err := c.Tpl.ExecuteTemplate(&b, BaseTpl, map[string]any{"Campaign": c}); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	return b.String()
}

func TestCompileTemplatePreheaderInTemplate(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeRichtext,
		PreviewText:  "Big news & <deals>",
		TemplateBody: `<html><body style="margin:0"><div>{{ template "content" . }}</div></body></html>`,
		Body:         `<p>Hello</p>`,
	}
	out := renderWithCampaign(t, c)

	if n := strings.Count(out, preheaderMarker); n != 1 {
		t.Fatalf("preheader blocks = %d, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, `<body style="margin:0"><div style="display:none;`) {
		t.Errorf("preheader not directly after <body>:\n%s", out)
	}
	if !strings.Contains(out, "Big news &amp; &lt;deals&gt;") {
		t.Errorf("preview text not HTML-escaped:\n%s", out)
	}
	if strings.Index(out, "Big news") > strings.Index(out, "Hello") {
		t.Errorf("preview text is not before the content:\n%s", out)
	}
}

// A visual campaign never renders its template: the block must land inside the
// visual document, and a template that mentions PreviewText must not count.
func TestCompileTemplatePreheaderVisual(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeVisual,
		PreviewText:  "Visual preview",
		TemplateBody: `<html><body>{{ .Campaign.PreviewText }}{{ template "content" . }}</body></html>`,
		Body:         `<!doctype html><html><body><p>Newsletter</p></body></html>`,
	}
	out := renderWithCampaign(t, c)

	if n := strings.Count(out, "Visual preview"); n != 1 {
		t.Fatalf("preview text occurrences = %d, want 1:\n%s", n, out)
	}
	if strings.Index(out, "Visual preview") < strings.Index(out, "<body>") {
		t.Errorf("preheader placed before the visual document's <body>:\n%s", out)
	}
}

// An html campaign carrying a full document with no template must not get the
// block in front of its doctype.
func TestCompileTemplatePreheaderHTMLDocumentWithoutTemplate(t *testing.T) {
	c := &Campaign{
		ContentType: CampaignContentTypeHTML,
		PreviewText: "Doc preview",
		Body:        `<!doctype html><html><body><p>Doc</p></body></html>`,
	}
	out := renderWithCampaign(t, c)

	if !strings.HasPrefix(out, "<!doctype html>") {
		t.Errorf("output does not start with the doctype:\n%s", out)
	}
	if n := strings.Count(out, "Doc preview"); n != 1 {
		t.Errorf("preview text occurrences = %d, want 1:\n%s", n, out)
	}
}

func TestCompileTemplateNoPreheaderWhenEmpty(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeRichtext,
		TemplateBody: `<html><body>{{ template "content" . }}</body></html>`,
		Body:         `<p>Hello</p>`,
	}
	if out := renderWithCampaign(t, c); strings.Contains(out, preheaderMarker) {
		t.Errorf("preheader injected with empty preview text:\n%s", out)
	}
}

func TestCompileTemplateNoPreheaderForPlain(t *testing.T) {
	c := &Campaign{
		ContentType: CampaignContentTypePlain,
		PreviewText: "ignored",
		Body:        "Hello",
	}
	if out := renderWithCampaign(t, c); strings.Contains(out, "ignored") {
		t.Errorf("preheader injected into a plain text campaign:\n%s", out)
	}
}
