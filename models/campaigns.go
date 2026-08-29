package models

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"strings"
	txttpl "text/template"

	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/types"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

const (
	CampaignStatusDraft         = "draft"
	CampaignStatusScheduled     = "scheduled"
	CampaignStatusRunning       = "running"
	CampaignStatusPaused        = "paused"
	CampaignStatusFinished      = "finished"
	CampaignStatusCancelled     = "cancelled"
	CampaignTypeRegular         = "regular"
	CampaignTypeOptin           = "optin"
	CampaignContentTypeRichtext = "richtext"
	CampaignContentTypeHTML     = "html"
	CampaignContentTypeMarkdown = "markdown"
	CampaignContentTypePlain    = "plain"
	CampaignContentTypeVisual   = "visual"
)

// Campaigns represents a slice of Campaigns.
type Campaigns []Campaign

// Campaign represents an e-mail campaign.
type Campaign struct {
	Base
	CampaignMeta

	UUID              string          `db:"uuid" json:"uuid"`
	Type              string          `db:"type" json:"type"`
	Name              string          `db:"name" json:"name"`
	Subject           string          `db:"subject" json:"subject"`
	FromEmail         string          `db:"from_email" json:"from_email"`
	Body              string          `db:"body" json:"body"`
	BodySource        null.String     `db:"body_source" json:"body_source"`
	AltBody           null.String     `db:"altbody" json:"altbody"`
	SendAt            null.Time       `db:"send_at" json:"send_at"`
	Status            string          `db:"status" json:"status"`
	ContentType       string          `db:"content_type" json:"content_type"`
	Tags              pq.StringArray  `db:"tags" json:"tags"`
	Headers           Headers         `db:"headers" json:"headers"`
	Attribs           JSON            `db:"attribs" json:"attribs"`
	TemplateID        null.Int        `db:"template_id" json:"template_id"`
	Messenger         string          `db:"messenger" json:"messenger"`
	Archive           bool            `db:"archive" json:"archive"`
	ArchiveSlug       null.String     `db:"archive_slug" json:"archive_slug"`
	ArchiveTemplateID null.Int        `db:"archive_template_id" json:"archive_template_id"`
	ArchiveMeta       json.RawMessage `db:"archive_meta" json:"archive_meta"`

	// TemplateBody is joined in from templates by the next-campaigns query.
	TemplateBody        string             `db:"template_body" json:"-"`
	ArchiveTemplateBody string             `db:"archive_template_body" json:"-"`
	Tpl                 *template.Template `json:"-"`
	SubjectTpl          *txttpl.Template   `json:"-"`
	AltBodyTpl          *template.Template `json:"-"`

	// HeaderTpls is holds optionally {{ templated }} campaign headers.
	HeaderTpls []map[string]*txttpl.Template `json:"-"`

	// List of media (attachment) IDs obtained from the next-campaign query
	// while sending a campaign.
	MediaIDs pq.Int64Array `json:"-" db:"media_id"`

	// Fetched bodies of the attachments.
	Attachments []Attachment `json:"-" db:"-"`

	// Pseudofield for getting the total number of subscribers
	// in searches and queries.
	Total int `db:"total" json:"-"`
}

// CampaignMeta contains fields tracking a campaign's progress.
type CampaignMeta struct {
	CampaignID int `db:"campaign_id" json:"-"`
	Views      int `db:"views" json:"views"`
	Clicks     int `db:"clicks" json:"clicks"`
	Bounces    int `db:"bounces" json:"bounces"`

	// Unique (per-subscriber) counts. These only cover events recorded while
	// privacy.individual_tracking was on; anonymous events are excluded.
	ViewsUnique   int `db:"views_unique" json:"views_unique"`
	ClicksUnique  int `db:"clicks_unique" json:"clicks_unique"`
	BouncesUnique int `db:"bounces_unique" json:"bounces_unique"`
	Unsubs        int `db:"unsubs" json:"unsubs"`

	// This is a list of {list_id, name} pairs unlike Subscriber.Lists[]
	// because lists can be deleted after a campaign is finished, resulting
	// in null lists data to be returned. For that reason, campaign_lists maintains
	// campaign-list associations with a historical record of id + name that persist
	// even after a list is deleted.
	Lists types.JSONText `db:"lists" json:"lists"`
	Media types.JSONText `db:"media" json:"media"`

	StartedAt null.Time `db:"started_at" json:"started_at"`
	ToSend    int       `db:"to_send" json:"to_send"`
	Sent      int       `db:"sent" json:"sent"`
}

// GetIDs returns the list of campaign IDs.
func (camps Campaigns) GetIDs() []int {
	IDs := make([]int, len(camps))
	for i, c := range camps {
		IDs[i] = c.ID
	}

	return IDs
}

// LoadStats lazy loads campaign stats onto a list of campaigns.
func (camps Campaigns) LoadStats(stmt *sqlx.Stmt) error {
	var meta []CampaignMeta
	if err := stmt.Select(&meta, pq.Array(camps.GetIDs())); err != nil {
		return err
	}

	if len(camps) != len(meta) {
		return errors.New("campaign stats count does not match")
	}

	for i, c := range meta {
		if c.CampaignID == camps[i].ID {
			camps[i].Lists = c.Lists
			camps[i].Views = c.Views
			camps[i].Clicks = c.Clicks
			camps[i].Bounces = c.Bounces
			camps[i].ViewsUnique = c.ViewsUnique
			camps[i].ClicksUnique = c.ClicksUnique
			camps[i].BouncesUnique = c.BouncesUnique
			camps[i].Unsubs = c.Unsubs
			camps[i].Media = c.Media
		}
	}

	return nil
}

// CompileTemplate compiles a campaign body template into its base
// template and sets the resultant template to Campaign.Tpl.
func (c *Campaign) CompileTemplate(f template.FuncMap) error {
	// If the subject line has a template string, compile it.
	if hasTplExpr(c.Subject) {
		subj := c.Subject
		for _, r := range regTplFuncs {
			subj = r.regExp.ReplaceAllString(subj, r.replace)
		}

		var txtFuncs map[string]any = f
		subjTpl, err := txttpl.New(ContentTpl).Funcs(txtFuncs).Parse(subj)
		if err != nil {
			return fmt.Errorf("error compiling subject: %v", err)
		}
		c.SubjectTpl = subjTpl
	}

	// Compile the base template.
	body := c.TemplateBody

	if body == "" || c.ContentType == CampaignContentTypeVisual {
		body = `{{ template "content" . }}`
	}

	// gunmade fork: add the open pixel and track the template's own links unless
	// the author already did. See models/autotrack.go.
	if tracksAsHTML(c.ContentType) {
		body = autoTrackView(body)
		body = autoTrackLinks(body)
	}

	// gunmade fork: guarantee a way out. Both the template and the campaign's
	// own content count, because either may carry the link — EXCEPT for visual
	// campaigns, which never render their template (the base body above is
	// replaced with a bare content include). A link that lives only in the
	// attached template never reaches a visual email, so the template must not
	// satisfy the check. This exact miss shipped visual campaigns with no
	// unsubscribe link. See models/autounsubscribe.go.
	needsUnsub := !hasUnsubscribe(c.Body) &&
		(c.ContentType == CampaignContentTypeVisual || !hasUnsubscribe(c.TemplateBody))
	if needsUnsub && c.ContentType != CampaignContentTypeVisual {
		if tracksAsHTML(c.ContentType) {
			body = autoUnsubscribeHTML(body)
		} else {
			body += unsubFooterText
		}
	}

	for _, r := range regTplFuncs {
		body = r.regExp.ReplaceAllString(body, r.replace)
	}

	baseTPL, err := template.New(BaseTpl).Funcs(f).Parse(body)
	if err != nil {
		return fmt.Errorf("error compiling base template: %v", err)
	}

	// If the format is markdown, convert Markdown to HTML.
	if c.ContentType == CampaignContentTypeMarkdown {
		var b bytes.Buffer
		if err := markdown.Convert([]byte(c.Body), &b); err != nil {
			return err
		}
		body = b.String()
	} else {
		body = c.Body
	}

	// gunmade fork: track the links in the campaign's own content. The pixel is
	// not added here — it belongs once, in the base template above.
	if tracksAsHTML(c.ContentType) {
		body = autoTrackLinks(body)
	}

	// gunmade fork: a visual body is a complete HTML document that renders
	// without the base template, so the guaranteed unsubscribe footer must
	// live inside that document, before its own </body> — a footer appended
	// to the base wrapper would land after </html>.
	if needsUnsub && c.ContentType == CampaignContentTypeVisual {
		body = autoUnsubscribeHTML(body)
	}

	// Compile the campaign message.
	for _, r := range regTplFuncs {
		body = r.regExp.ReplaceAllString(body, r.replace)
	}

	msgTpl, err := template.New(ContentTpl).Funcs(f).Parse(body)
	if err != nil {
		return fmt.Errorf("error compiling message: %v", err)
	}

	out, err := baseTPL.AddParseTree(ContentTpl, msgTpl.Tree)
	if err != nil {
		return fmt.Errorf("error inserting child template: %v", err)
	}
	c.Tpl = out

	// gunmade fork: the plain text alternative needs the same way out. A reader
	// on a text-only client sees this part and nothing else. Appending the
	// footer also introduces a template expression, so an alt body that was
	// previously static now compiles — which is why this runs before the check
	// below. Repeating the call is safe: hasUnsubscribe is true afterwards.
	if c.AltBody.Valid && c.AltBody.String != "" && !hasUnsubscribe(c.AltBody.String) {
		c.AltBody.String += unsubFooterText
	}

	if hasTplExpr(c.AltBody.String) {
		b := c.AltBody.String
		for _, r := range regTplFuncs {
			b = r.regExp.ReplaceAllString(b, r.replace)
		}
		bTpl, err := template.New(ContentTpl).Funcs(f).Parse(b)
		if err != nil {
			return fmt.Errorf("error compiling alt plaintext message: %v", err)
		}
		c.AltBodyTpl = bTpl
	}

	// Compile any header values that contain template expressions.
	for _, set := range c.Headers {
		for _, val := range set {
			if hasTplExpr(val) {
				c.HeaderTpls = make([]map[string]*txttpl.Template, len(c.Headers))
				break
			}
		}
		if c.HeaderTpls != nil {
			break
		}
	}
	if c.HeaderTpls != nil {
		var txtFuncs map[string]any = f
		for i, set := range c.Headers {
			c.HeaderTpls[i] = make(map[string]*txttpl.Template, len(set))
			for hdr, val := range set {
				if !hasTplExpr(val) {
					continue
				}
				tpl, err := txttpl.New(ContentTpl).Funcs(txtFuncs).Parse(val)
				if err != nil {
					return fmt.Errorf("error compiling header %q: %v", hdr, err)
				}
				c.HeaderTpls[i][hdr] = tpl
			}
		}
	}

	return nil
}

// hasTplExpr checks whether a given string has a Go template expression with {{ and  }}.
func hasTplExpr(s string) bool {
	_, after, ok := strings.Cut(s, "{{")
	return ok && strings.Contains(after, "}}")
}

// ConvertContent converts a campaign's body from one format to another,
// for example, Markdown to HTML.
func (c *Campaign) ConvertContent(from, to string) (string, error) {
	body := c.Body
	for _, r := range regTplFuncs {
		body = r.regExp.ReplaceAllString(body, r.replace)
	}

	// If the format is markdown, convert Markdown to HTML.
	var out string
	if from == CampaignContentTypeMarkdown &&
		(to == CampaignContentTypeHTML || to == CampaignContentTypeRichtext) {
		var b bytes.Buffer
		if err := markdown.Convert([]byte(c.Body), &b); err != nil {
			return out, err
		}
		out = b.String()
	} else {
		return out, errors.New("unknown formats to convert")
	}

	return out, nil
}

// CampaignAnalyticsSummary holds lifetime aggregate engagement counts and
// computed rates for a single campaign.
type CampaignAnalyticsSummary struct {
	CampaignID int `db:"-" json:"campaign_id"`
	Sent       int `db:"-" json:"sent"`
	ToSend     int `db:"-" json:"to_send"`

	// Delivered = sent - bounced (distinct hard/soft bounced subscribers).
	Delivered int `db:"-" json:"delivered"`

	ViewsTotal   int `db:"views_total" json:"views_total"`
	ViewsUnique  int `db:"views_unique" json:"views_unique"`
	ClicksTotal  int `db:"clicks_total" json:"clicks_total"`
	ClicksUnique int `db:"clicks_unique" json:"clicks_unique"`
	Bounced      int `db:"bounced" json:"bounced"`
	BouncedHard  int `db:"bounced_hard" json:"bounced_hard"`
	BouncedSoft  int `db:"bounced_soft" json:"bounced_soft"`
	Complaints   int `db:"complaints" json:"complaints"`
	Unsubs       int `db:"unsubs" json:"unsubs"`

	// IndividualTracking indicates whether unique counts and the rates derived
	// from them are meaningful under the current privacy settings.
	IndividualTracking bool `db:"-" json:"individual_tracking"`

	// Rates are percentages over delivered. They are null when individual
	// tracking is off, as unique counts are then unavailable.
	OpenRate    *float64 `db:"-" json:"open_rate"`
	ClickRate   *float64 `db:"-" json:"click_rate"`
	ClickToOpen *float64 `db:"-" json:"click_to_open_rate"`
	BounceRate  float64  `db:"-" json:"bounce_rate"`
	UnsubRate   float64  `db:"-" json:"unsub_rate"`
}

// CampaignLinkStat is one URL's click stats for a campaign.
type CampaignLinkStat struct {
	URL    string `db:"url" json:"url"`
	Total  int    `db:"total" json:"total"`
	Unique int    `db:"unique_subs" json:"unique"`
}

// CampaignSubscriberActivity is one subscriber row in a campaign activity
// drill-down list (opened / clicked / didn't open / unsubscribed).
type CampaignSubscriberActivity struct {
	Total   int       `db:"total" json:"-"`
	ID      int       `db:"id" json:"id"`
	UUID    string    `db:"uuid" json:"uuid"`
	Email   string    `db:"email" json:"email"`
	Name    string    `db:"name" json:"name"`
	Status  string    `db:"status" json:"status"`
	FirstAt null.Time `db:"first_at" json:"first_at"`
	Num     int       `db:"num" json:"count"`
}
