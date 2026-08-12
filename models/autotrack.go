package models

import (
	"regexp"
	"strings"
)

// Automatic open and click tracking (gunmade fork).
//
// Stock listmonk tracks a campaign only where the author remembered to ask for
// it: a template must carry {{ TrackView }} for the open pixel, and every link
// must be written as https://example.com@TrackLink to be counted. A template
// built from hand-written HTML has neither, so the campaign reports zero opens
// and zero clicks while sending perfectly well. That failure is silent, and it
// is only visible after the send, when the data is already unrecoverable.
//
// These two functions inject both by default. They run inside CompileTemplate,
// before the regTplFuncs substitution, so the output is the same shorthand a
// person would have typed and the existing template machinery handles the rest.

const trackViewTag = `{{ TrackView }}`

var (
	// An anchor tag, opening tag only. Matched whole so that attributes other
	// than href are visible to the opt-out check.
	reAnchorTag = regexp.MustCompile(`(?is)<a\b[^>]*>`)

	// A literal http(s) URL in an href. Template expressions such as
	// {{ UnsubscribeURL }} do not start with a scheme, so they never match and
	// are never rewritten. That is deliberate: an unsubscribe link routed
	// through the click tracker would record a click for someone leaving.
	reHrefURL = regexp.MustCompile(`(?is)(\bhref\s*=\s*["'])(https?://[^"']*)(["'])`)

	reBodyClose = regexp.MustCompile(`(?i)</body\s*>`)
)

// autoTrackLinks suffixes @TrackLink to every plain http(s) link in an anchor
// tag, which regTplFuncs then rewrites into a {{ TrackLink }} call.
//
// A link is left alone when it already carries @TrackLink, when it contains a
// template expression, or when its anchor tag carries data-no-track. The last
// is the escape hatch: mark a link with data-no-track to keep it out of the
// click report.
func autoTrackLinks(s string) string {
	if s == "" {
		return s
	}

	return reAnchorTag.ReplaceAllStringFunc(s, func(tag string) string {
		if strings.Contains(strings.ToLower(tag), "data-no-track") {
			return tag
		}

		return reHrefURL.ReplaceAllStringFunc(tag, func(href string) string {
			m := reHrefURL.FindStringSubmatch(href)
			if m == nil {
				return href
			}

			url := m[2]
			if strings.Contains(url, "@TrackLink") || strings.Contains(url, "{{") {
				return href
			}

			return m[1] + url + "@TrackLink" + m[3]
		})
	})
}

// autoTrackView places the open-tracking pixel in a template that lacks one.
// The pixel goes immediately before </body> so it inherits nothing from the
// surrounding layout; templates without a </body> get it appended.
//
// A template that already mentions TrackView is left untouched, so anyone who
// positioned the pixel by hand keeps their placement.
func autoTrackView(s string) string {
	if strings.Contains(s, "TrackView") {
		return s
	}

	if loc := reBodyClose.FindStringIndex(s); loc != nil {
		return s[:loc[0]] + trackViewTag + s[loc[0]:]
	}

	return s + trackViewTag
}

// tracksAsHTML reports whether a campaign's content type renders to HTML.
// A plain text campaign gets neither an image pixel nor a rewritten link,
// because both would be delivered to the reader as visible text.
func tracksAsHTML(contentType string) bool {
	return contentType != CampaignContentTypePlain
}
