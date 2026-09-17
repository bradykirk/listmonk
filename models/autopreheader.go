package models

import (
	"regexp"
	"strings"
)

// Campaign preview text (gunmade fork).
//
// Inboxes show a short line after the subject, taken from the first text in
// the message. preheaderHTML puts the campaign's preview_text there and hides
// it in the opened email. The zero-width padding stops the inbox from running
// on into the start of the body copy.
//
// Injection happens at compile time from the campaign's own column, so it
// reaches every existing draft, clone and visual campaign without editing
// stored templates or bodies. models/autounsubscribe.go uses the same approach
// for the unsubscribe footer.

const preheaderPad = `&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;` +
	`&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;`

// html/template escapes the value, so preview text cannot inject markup.
const preheaderHTML = `<div style="display:none;font-size:1px;color:#ffffff;line-height:1px;` +
	`max-height:0;max-width:0;opacity:0;overflow:hidden;mso-hide:all;">` +
	`{{ .Campaign.PreviewText }}` + preheaderPad + `</div>`

var reBodyOpen = regexp.MustCompile(`(?i)<body\b[^>]*>`)

type preheaderTarget int

const (
	preheaderNone preheaderTarget = iota
	preheaderBase
	preheaderContent
)

// hasPreheader reports whether an author already placed the preview text.
func hasPreheader(sources ...string) bool {
	for _, s := range sources {
		if strings.Contains(s, ".Campaign.PreviewText") {
			return true
		}
	}

	return false
}

// choosePreheaderTarget decides where the preview text goes. base is the base
// template body as CompileTemplate will parse it. The block belongs right after
// the first opening <body> tag the email contains: the base template's, else
// the campaign content's. With neither, it leads the base template. Visual
// campaigns arrive with base already replaced by the bare content include, so
// their attached template never counts.
func choosePreheaderTarget(c *Campaign, base string) preheaderTarget {
	if strings.TrimSpace(c.PreviewText) == "" || !tracksAsHTML(c.ContentType) {
		return preheaderNone
	}
	if hasPreheader(base, c.Body) {
		return preheaderNone
	}
	if reBodyOpen.MatchString(base) {
		return preheaderBase
	}
	if reBodyOpen.MatchString(c.Body) {
		return preheaderContent
	}

	return preheaderBase
}

// autoPreheaderHTML inserts the block immediately after the first opening
// <body> tag, or at the start when there is none.
func autoPreheaderHTML(s string) string {
	if loc := reBodyOpen.FindStringIndex(s); loc != nil {
		return s[:loc[1]] + preheaderHTML + s[loc[1]:]
	}

	return preheaderHTML + s
}
