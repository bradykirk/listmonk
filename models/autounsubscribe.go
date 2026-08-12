package models

import "strings"

// Guaranteed unsubscribe link (gunmade fork).
//
// listmonk validates one thing about a campaign template: that it contains
// {{ template "content" . }}. It does not check for an unsubscribe link. The
// stock templates carry one, so the omission only appears when a template is
// written by hand — and then the campaign sends to the whole list with no way
// out in the body.
//
// That is not a cosmetic problem. CAN-SPAM requires a clear unsubscribe notice
// in the message. Gmail and Yahoo require one-click unsubscribe from any sender
// above 5,000 messages a day. A reader with no visible link marks the message
// as spam instead, and complaints are the fastest route to an SES suspension.
//
// The List-Unsubscribe header, controlled by privacy.unsubscribe_header, is a
// separate mechanism and is not a substitute: it renders as a client-provided
// button, and CAN-SPAM asks for a notice inside the message.

// A footer used only when the template supplies none. Inline styles, because
// email clients discard <style> blocks.
const unsubFooterHTML = `<div style="margin:24px 0 0;padding:16px 8px 8px;` +
	`border-top:1px solid #e8e8e8;font-family:Arial,Helvetica,sans-serif;` +
	`font-size:12px;line-height:1.5;color:#888;text-align:center">` +
	`You received this email because you subscribed to our list.<br />` +
	`<a href="{{ UnsubscribeURL }}" style="color:#888;text-decoration:underline">` +
	`Unsubscribe</a></div>`

const unsubFooterText = "\n\n---\nYou received this email because you subscribed to our list.\n" +
	"Unsubscribe: {{ UnsubscribeURL }}\n"

// hasUnsubscribe reports whether a piece of source already offers a way out.
// ManageURL counts: listmonk's preferences page carries an unsubscribe control,
// so a template linking to it is not leaving the reader stranded.
func hasUnsubscribe(sources ...string) bool {
	for _, s := range sources {
		if strings.Contains(s, "UnsubscribeURL") || strings.Contains(s, "ManageURL") {
			return true
		}
	}

	return false
}

// autoUnsubscribeHTML places the footer immediately before </body>, or appends
// it when the template has no </body>.
func autoUnsubscribeHTML(s string) string {
	if loc := reBodyClose.FindStringIndex(s); loc != nil {
		return s[:loc[0]] + unsubFooterHTML + s[loc[0]:]
	}

	return s + unsubFooterHTML
}
