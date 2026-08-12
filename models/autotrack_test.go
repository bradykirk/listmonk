package models

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestAutoTrackView(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "inserts before closing body",
			in:   `<html><body><p>hi</p></body></html>`,
			want: `<html><body><p>hi</p>{{ TrackView }}</body></html>`,
		},
		{
			name: "appends when there is no body tag",
			in:   `<p>hi</p>`,
			want: `<p>hi</p>{{ TrackView }}`,
		},
		{
			name: "tolerates whitespace in the closing tag",
			in:   `<body>x</body >`,
			want: `<body>x{{ TrackView }}</body >`,
		},
		{
			name: "leaves a hand-placed pixel alone",
			in:   `<body>{{ TrackView }}<p>hi</p></body>`,
			want: `<body>{{ TrackView }}<p>hi</p></body>`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := autoTrackView(c.in); got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}

func TestAutoTrackLinks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "tracks a plain link",
			in:   `<a href="https://gunmade.com/deals">Deals</a>`,
			want: `<a href="https://gunmade.com/deals@TrackLink">Deals</a>`,
		},
		{
			name: "keeps query strings intact",
			in:   `<a href="https://gunmade.com/p?id=7&x=2">P</a>`,
			want: `<a href="https://gunmade.com/p?id=7&x=2@TrackLink">P</a>`,
		},
		{
			name: "handles single quotes",
			in:   `<a href='https://gunmade.com'>G</a>`,
			want: `<a href='https://gunmade.com@TrackLink'>G</a>`,
		},
		{
			name: "does not double-track",
			in:   `<a href="https://gunmade.com@TrackLink">G</a>`,
			want: `<a href="https://gunmade.com@TrackLink">G</a>`,
		},
		{
			name: "never rewrites the unsubscribe link",
			in:   `<a href="{{ UnsubscribeURL }}">Unsubscribe</a>`,
			want: `<a href="{{ UnsubscribeURL }}">Unsubscribe</a>`,
		},
		{
			name: "leaves a templated root url alone",
			in:   `<a href="{{ RootURL }}/x">X</a>`,
			want: `<a href="{{ RootURL }}/x">X</a>`,
		},
		{
			name: "honours the opt-out attribute",
			in:   `<a data-no-track href="https://gunmade.com">G</a>`,
			want: `<a data-no-track href="https://gunmade.com">G</a>`,
		},
		{
			name: "ignores mailto and tel",
			in:   `<a href="mailto:a@b.com">M</a><a href="tel:+1555">T</a>`,
			want: `<a href="mailto:a@b.com">M</a><a href="tel:+1555">T</a>`,
		},
		{
			name: "does not touch image sources",
			in:   `<img src="https://assets.gunmade.com/a.png" />`,
			want: `<img src="https://assets.gunmade.com/a.png" />`,
		},
		{
			name: "tracks several links in one block",
			in:   `<a href="https://a.com">A</a> and <a href="https://b.com">B</a>`,
			want: `<a href="https://a.com@TrackLink">A</a> and <a href="https://b.com@TrackLink">B</a>`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := autoTrackLinks(c.in); got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}

// stubFuncs stands in for the real tracking functions, which need a running
// manager. The markers are enough to prove the template called them.
func stubFuncs() template.FuncMap {
	return template.FuncMap{
		"TrackView": func(_ any) template.HTML {
			return template.HTML(`<img src="PIXEL" />`)
		},
		// Must return a real http(s) URL. Go's html/template autoescaper
		// replaces any unknown URL scheme in an href with #ZgotmplZ, so a
		// marker like "TRACKED:..." would be silently dropped by the renderer
		// rather than by the code under test.
		"TrackLink": func(url string, _ any) string {
			return "https://lists.gunmade.com/link/xxxx?to=" + url
		},
		"UnsubscribeURL": func(_ any) string { return "https://lists.gunmade.com/unsub" },
		"Safe":           func(s string) template.HTML { return template.HTML(s) },
	}
}

// The end-to-end check: a template and a body that ask for no tracking at all
// must still render a pixel and a redirected link.
func TestCompileTemplateInjectsTracking(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeHTML,
		TemplateBody: `<html><body><div>{{ template "content" . }}</div><a href="{{ UnsubscribeURL }}">Out</a></body></html>`,
		Body:         `<p><a href="https://gunmade.com/tx22">TX22</a></p>`,
	}

	if err := c.CompileTemplate(stubFuncs()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	var b bytes.Buffer
	if err := c.Tpl.ExecuteTemplate(&b, BaseTpl, nil); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	out := b.String()

	if !strings.Contains(out, "PIXEL") {
		t.Errorf("open pixel missing from output:\n%s", out)
	}
	if !strings.Contains(out, "lists.gunmade.com/link/xxxx") {
		t.Errorf("campaign link was not tracked:\n%s", out)
	}
	if strings.Count(out, "PIXEL") != 1 {
		t.Errorf("expected exactly one pixel, got %d:\n%s", strings.Count(out, "PIXEL"), out)
	}
	if strings.Contains(out, "to=https://lists.gunmade.com/unsub") {
		t.Errorf("unsubscribe link must not be tracked:\n%s", out)
	}
}

// A plain text campaign must come out clean: an <img> tag or a redirect URL
// would be delivered to the reader as visible text.
func TestCompileTemplateSkipsPlainText(t *testing.T) {
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

	if out := b.String(); strings.Contains(out, "PIXEL") || strings.Contains(out, "/link/xxxx") {
		t.Errorf("plain text campaign was modified:\n%s", out)
	}
}
