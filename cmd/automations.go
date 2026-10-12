package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/automations"
	"github.com/knadh/listmonk/internal/brands"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

// FORK(hcm): automation step campaigns are edited on the Automations page.

const msgAutomationEmail = "This campaign is an automation email. Change it on the Automations page."

// stepCampaignGuard checks an edit of a campaign that may be an automation step.
// It returns nil for a campaign that is not a step.
func (a *App) stepCampaignGuard(c echo.Context, id int, req *campReq) error {
	info, ok, err := a.automations.StepOwner(id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error checking automation step")
	}
	if !ok {
		return nil
	}

	if len(req.ListIDs) != 1 || req.ListIDs[0] != info.ListID {
		return echo.NewHTTPError(http.StatusBadRequest, "An automation email must stay on the automation list.")
	}
	if !info.Enabled {
		return nil
	}

	user := auth.GetUser(c)
	if !user.HasPerm(auth.PermCampaignsSend) {
		return echo.NewHTTPError(http.StatusForbidden, "Sending permission (campaigns:send) is needed to change the email of an enabled automation.")
	}
	for _, f := range []string{req.Subject, req.Body, req.AltBody.String, req.PreviewText} {
		if strings.Contains(f, automations.Marker) {
			return echo.NewHTTPError(http.StatusBadRequest, "Replace [[SENDER_INFO]] before you save an enabled automation email.")
		}
	}

	tplID := 0
	if req.TemplateID.Valid {
		tplID = int(req.TemplateID.Int)
	}
	if err := a.brands.GuardSender([]int{info.ListID}, req.Messenger, req.FromEmail,
		brands.TemplateRef{ID: tplID, Visual: req.ContentType == models.CampaignContentTypeVisual}); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return nil
}

// FORK(hcm): automations API (spec sections 6.1, 6.4, 6.5 and 7).

const (
	autoNameMaxLen  = 200
	autoMaxDelayMin = 525600 // One year.
)

type autoCreateReq struct {
	Name      string `json:"name"`
	ListID    int    `json:"list_id"`
	Messenger string `json:"messenger"`
	FromEmail string `json:"from_email"`
}

type autoUpdateReq struct {
	Name    string `json:"name"`
	ListID  int    `json:"list_id"`
	Enabled bool   `json:"enabled"`
	Steps   []struct {
		Position     int `json:"position"`
		DelayMinutes int `json:"delay_minutes"`
	} `json:"steps"`
}

type autoEnrollReq struct {
	SubscriberID int `json:"subscriber_id"`
	ListID       int `json:"list_id"`
}

type autoCounts struct {
	Enrolled int `json:"enrolled"`
	Sent     int `json:"sent"`
	Active   int `json:"active"`
	Failed   int `json:"failed"`
	Unknown  int `json:"unknown"`
}

type autoStepCampaign struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Subject   string `json:"subject"`
	Messenger string `json:"messenger"`
	FromEmail string `json:"from_email"`
	Views     int    `json:"views"`
	Clicks    int    `json:"clicks"`
	Bounces   int    `json:"bounces"`
}

type autoStepOut struct {
	Position     int              `json:"position"`
	DelayMinutes int              `json:"delay_minutes"`
	CampaignID   int              `json:"campaign_id"`
	Campaign     autoStepCampaign `json:"campaign"`
	Sent         int              `json:"sent"`
}

type autoListItem struct {
	automations.Summary
	Brand string `json:"brand"`
}

// GetAutomations lists the automations, limited to the selected brand's lists.
func (a *App) GetAutomations(c echo.Context) error {
	var listIDs []int
	if _, ids, scoped := brands.BrandFrom(c); scoped {
		listIDs = ids
		if listIDs == nil {
			// A nil slice means all lists; a brand with no lists sees none.
			listIDs = []int{}
		}
	}
	all, err := a.automations.All(listIDs)
	if err != nil {
		a.log.Printf("error fetching automations: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "error fetching automations")
	}

	bs, err := a.brands.Store.All()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading brands")
	}
	tags, err := a.brands.Store.ListTags()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading lists")
	}

	out := make([]autoListItem, 0, len(all))
	for _, s := range all {
		// Like campaigns, a user without the *_all list permissions sees only permitted lists.
		if !userHasList(c, s.ListID) {
			continue
		}
		out = append(out, autoListItem{Summary: s, Brand: brands.BrandOfList(tags[s.ListID], bs).Slug})
	}
	return c.JSON(http.StatusOK, okResp{out})
}

// GetAutomation returns one automation with its steps and counts.
func (a *App) GetAutomation(c echo.Context) error {
	auto, steps, err := a.automationInBrand(c, getID(c))
	if err != nil {
		return err
	}

	outSteps := make([]autoStepOut, 0, len(steps))
	for _, st := range steps {
		camp, err := a.core.GetCampaign(st.CampaignID, "", "")
		if err != nil {
			return err
		}
		sent, err := a.automations.SentByCampaign(st.CampaignID)
		if err != nil {
			a.log.Printf("error counting automation sends: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "error counting automation sends")
		}
		outSteps = append(outSteps, autoStepOut{
			Position:     st.Position,
			DelayMinutes: st.DelayMinutes,
			CampaignID:   st.CampaignID,
			Campaign: autoStepCampaign{
				ID: camp.ID, Name: camp.Name, Subject: camp.Subject, Messenger: camp.Messenger,
				FromEmail: camp.FromEmail, Views: camp.Views, Clicks: camp.Clicks, Bounces: camp.Bounces,
			},
			Sent: sent,
		})
	}

	var counts autoCounts
	sums, err := a.automations.All([]int{auto.ListID})
	if err != nil {
		a.log.Printf("error fetching automation counts: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "error fetching automation counts")
	}
	for _, s := range sums {
		if s.ID == auto.ID {
			counts = autoCounts{Enrolled: s.Enrolled, Sent: s.Sent, Active: s.Active, Failed: s.Failed, Unknown: s.Unknown}
		}
	}

	return c.JSON(http.StatusOK, okResp{map[string]any{
		"automation": auto,
		"steps":      outSteps,
		"counts":     counts,
	}})
}

// CreateAutomation creates a disabled automation and its step 1 hidden campaign.
func (a *App) CreateAutomation(c echo.Context) error {
	var req autoCreateReq
	if err := c.Bind(&req); err != nil {
		return err
	}
	req.Name = strings.TrimSpace(req.Name)
	if !strHasLen(req.Name, 1, autoNameMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, "The name must have 1 to 200 characters.")
	}

	list, err := a.listOr404(req.ListID)
	if err != nil {
		return err
	}
	if !listInSelectedBrand(c, list.ID) {
		return echo.NewHTTPError(http.StatusBadRequest, "Select a list of this brand.")
	}
	if err := a.automationListPerm(c, list.ID); err != nil {
		return err
	}

	// Messenger default: the first messenger of the list's brand, else "email".
	messenger := strings.TrimSpace(req.Messenger)
	if messenger == "" {
		messenger = "email"
		bs, err := a.brands.Store.All()
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "error loading brands")
		}
		if lb := brands.BrandOfList(list.Tags, bs); lb.Slug != "" {
			b, ok, err := a.brands.Store.Get(lb.Slug)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "error loading brand")
			}
			if ok && len(b.Messengers) > 0 {
				messenger = b.Messengers[0]
			}
		}
	}

	tplID, err := a.brands.Store.DefaultTemplateID()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading the default template")
	}

	// Validate through the upstream campaign rules (from address, messenger, name).
	o := campReq{
		Campaign: models.Campaign{
			Type:        models.CampaignTypeRegular,
			Name:        "Automation: " + req.Name,
			Subject:     "Welcome",
			FromEmail:   strings.TrimSpace(req.FromEmail),
			Body:        "",
			ContentType: models.CampaignContentTypeRichtext,
			Messenger:   messenger,
			Archive:     false,
			Attribs:     models.JSON{},
			ArchiveMeta: json.RawMessage("{}"),
		},
		ListIDs: []int{list.ID},
	}
	if tplID > 0 {
		o.TemplateID = null.IntFrom(tplID)
	}
	o, err = a.validateCampaignFields(o)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	camp, err := a.core.CreateCampaign(o.Campaign, o.ListIDs, nil)
	if err != nil {
		return err
	}

	auto, err := a.automations.Create(req.Name, list.ID, camp.ID)
	if err != nil {
		a.log.Printf("error creating automation: %v", err)
		if derr := a.core.DeleteCampaign(camp.ID); derr != nil {
			a.log.Printf("error deleting campaign %d of a failed automation: %v", camp.ID, derr)
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "error creating automation")
	}

	return c.JSON(http.StatusOK, okResp{auto})
}

// UpdateAutomation changes the name, list, step delays and enabled state.
func (a *App) UpdateAutomation(c echo.Context) error {
	id := getID(c)
	cur, steps, err := a.automationInBrand(c, id)
	if err != nil {
		return err
	}

	var req autoUpdateReq
	if err := c.Bind(&req); err != nil {
		return err
	}
	req.Name = strings.TrimSpace(req.Name)
	if !strHasLen(req.Name, 1, autoNameMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, "The name must have 1 to 200 characters.")
	}
	if req.ListID == 0 {
		req.ListID = cur.ListID
	}
	if req.ListID != cur.ListID {
		if _, err := a.listOr404(req.ListID); err != nil {
			return err
		}
		if !listInSelectedBrand(c, req.ListID) {
			return echo.NewHTTPError(http.StatusBadRequest, "Select a list of this brand.")
		}
		if err := a.automationListPerm(c, req.ListID); err != nil {
			return err
		}
	}
	upd := make([]automations.Step, 0, len(req.Steps))
	for _, st := range req.Steps {
		if st.DelayMinutes < 0 || st.DelayMinutes > autoMaxDelayMin {
			return echo.NewHTTPError(http.StatusBadRequest, "The delay must be 0 to 525600 minutes.")
		}
		upd = append(upd, automations.Step{Position: st.Position, DelayMinutes: st.DelayMinutes})
	}

	// Enable rules (6.4, 6.5) are checked before any change is written.
	if req.Enabled {
		user := auth.GetUser(c)
		if !user.HasPerm(auth.PermCampaignsSend) {
			return echo.NewHTTPError(http.StatusForbidden, "Sending permission (campaigns:send) is needed to enable an automation.")
		}
		if err := a.automationEnableCheck(req.ListID, steps); err != nil {
			return err
		}
	}

	if err := a.automations.Update(id, req.Name, req.ListID, upd); err != nil {
		return a.automationStoreErr(err)
	}
	if err := a.automations.SetEnabled(id, req.Enabled); err != nil {
		return a.automationStoreErr(err)
	}

	auto, _, err := a.automations.Get(id)
	if err != nil {
		return a.automationStoreErr(err)
	}
	return c.JSON(http.StatusOK, okResp{auto})
}

// automationEnableCheck runs the enable rules on the stored step campaigns.
func (a *App) automationEnableCheck(listID int, steps []automations.Step) error {
	camps := make([]models.Campaign, 0, len(steps))
	content := make([]automations.StepContent, 0, len(steps))
	for _, st := range steps {
		camp, err := a.core.GetCampaign(st.CampaignID, "", "")
		if err != nil {
			return err
		}
		camps = append(camps, camp)
		content = append(content, automations.StepContent{
			Subject: camp.Subject, Body: camp.Body, AltBody: camp.AltBody.String, PreviewText: camp.PreviewText,
		})
	}
	if p := automations.EnableProblems(content); p != "" {
		return echo.NewHTTPError(http.StatusBadRequest, p)
	}
	for i, camp := range camps {
		if err := a.brands.GuardSender([]int{listID}, camp.Messenger, camp.FromEmail, campaignTemplateRef(camp)); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Step %d: %s", i+1, err.Error()))
		}
	}
	return nil
}

// DeleteAutomation deletes a disabled automation, then its step campaigns.
func (a *App) DeleteAutomation(c echo.Context) error {
	id := getID(c)
	if _, _, err := a.automationInBrand(c, id); err != nil {
		return err
	}

	campIDs, err := a.automations.Delete(id)
	if err != nil {
		if errors.Is(err, automations.ErrEnabled) {
			return echo.NewHTTPError(http.StatusBadRequest, "Disable the automation first.")
		}
		return a.automationStoreErr(err)
	}

	// The step rows are gone, so ON DELETE RESTRICT no longer applies.
	var failed []int
	for _, cid := range campIDs {
		if err := a.core.DeleteCampaign(cid); err != nil {
			a.log.Printf("error deleting step campaign %d of automation %d: %v", cid, id, err)
			failed = append(failed, cid)
		}
	}
	if len(failed) > 0 {
		return echo.NewHTTPError(http.StatusInternalServerError,
			fmt.Sprintf("The automation is deleted, but these campaigns are not: %v", failed))
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// EnrollAutomation enrolls a subscriber in the enabled automation of a list (spec 6.1).
func (a *App) EnrollAutomation(c echo.Context) error {
	var req autoEnrollReq
	if err := c.Bind(&req); err != nil {
		return err
	}

	if _, err := a.listOr404(req.ListID); err != nil {
		return err
	}
	if req.SubscriberID < 1 {
		return echo.NewHTTPError(http.StatusNotFound, a.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.subscriber}"))
	}
	if _, err := a.core.GetSubscriber(req.SubscriberID, "", ""); err != nil {
		return as404(err)
	}
	user := auth.GetUser(c)
	if err := user.HasListPerm(auth.PermTypeManage, req.ListID); err != nil {
		return echo.NewHTTPError(http.StatusForbidden, a.i18n.Ts("globals.messages.permissionDenied", "name", "list"))
	}
	if !listInSelectedBrand(c, req.ListID) {
		return echo.NewHTTPError(http.StatusNotFound, a.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.list}"))
	}

	enrolled, reason, err := a.automations.Enroll(req.SubscriberID, req.ListID)
	if err != nil {
		a.log.Printf("error enrolling subscriber %d on list %d: %v", req.SubscriberID, req.ListID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "error enrolling subscriber")
	}
	return c.JSON(http.StatusOK, okResp{map[string]any{"enrolled": enrolled, "reason": reason}})
}

// automationInBrand loads an automation. It returns 404 when the automation
// is missing or, with a brand selected, its list is outside the brand.
func (a *App) automationInBrand(c echo.Context, id int) (automations.Automation, []automations.Step, error) {
	auto, steps, err := a.automations.Get(id)
	if err != nil {
		return auto, nil, a.automationStoreErr(err)
	}
	if !listInSelectedBrand(c, auto.ListID) {
		return auto, nil, echo.NewHTTPError(http.StatusNotFound, "The automation was not found.")
	}
	if err := a.automationListPerm(c, auto.ListID); err != nil {
		return auto, nil, err
	}
	return auto, steps, nil
}

// automationListPerm returns 403 when the user has no get or manage permission
// on the list. This is the list check that campaign routes do (checkCampaignPerm).
func (a *App) automationListPerm(c echo.Context, listID int) error {
	if !userHasList(c, listID) {
		return echo.NewHTTPError(http.StatusForbidden, a.i18n.Ts("globals.messages.permissionDenied", "name", "list"))
	}
	return nil
}

// userHasList reports whether the user may get or manage the list, as
// checkCampaignPerm decides it.
func userHasList(c echo.Context, listID int) bool {
	user := auth.GetUser(c)
	hasAll, ids := user.GetPermittedLists(auth.PermTypeGet | auth.PermTypeManage)
	if hasAll {
		return true
	}
	for _, id := range ids {
		if id == listID {
			return true
		}
	}
	return false
}

// automationStoreErr maps automations store errors to HTTP errors.
func (a *App) automationStoreErr(err error) error {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return echo.NewHTTPError(http.StatusNotFound, "The automation was not found.")
	case errors.Is(err, automations.ErrEnabledOnList):
		return echo.NewHTTPError(http.StatusConflict, "Another automation on this list is enabled.")
	case errors.Is(err, automations.ErrHasEnrollments):
		return echo.NewHTTPError(http.StatusBadRequest, "The list cannot change after the first enrollment.")
	case errors.Is(err, automations.ErrEnabled):
		return echo.NewHTTPError(http.StatusBadRequest, "Disable the automation first.")
	}
	a.log.Printf("automations store error: %v", err)
	return echo.NewHTTPError(http.StatusInternalServerError, "error updating automation")
}

// listOr404 loads a list. A missing list is 404.
func (a *App) listOr404(id int) (models.List, error) {
	if id < 1 {
		return models.List{}, echo.NewHTTPError(http.StatusNotFound, a.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.list}"))
	}
	l, err := a.core.GetList(id, "")
	if err != nil {
		return l, as404(err)
	}
	return l, nil
}

// as404 turns core's "not found" (400) into 404. Other errors pass through.
func as404(err error) error {
	var he *echo.HTTPError
	if errors.As(err, &he) && he.Code == http.StatusBadRequest {
		return echo.NewHTTPError(http.StatusNotFound, he.Message)
	}
	return err
}

// listInSelectedBrand reports whether listID is in the selected brand. It is
// true for All brands.
func listInSelectedBrand(c echo.Context, listID int) bool {
	_, ids, scoped := brands.BrandFrom(c)
	if !scoped {
		return true
	}
	for _, id := range ids {
		if id == listID {
			return true
		}
	}
	return false
}

// campaignTemplateRef is the send guard template reference of a campaign.
func campaignTemplateRef(camp models.Campaign) brands.TemplateRef {
	tplID := 0
	if camp.TemplateID.Valid {
		tplID = camp.TemplateID.Int
	}
	return brands.TemplateRef{ID: tplID, Visual: camp.ContentType == models.CampaignContentTypeVisual}
}

// automationMessage is a rendered step campaign message for one subscriber.
type automationMessage struct {
	mgr *manager.Manager
	msg manager.CampaignMessage
}

func (m automationMessage) Content() string {
	return m.msg.Subject() + "\n" + string(m.msg.Body()) + "\n" + string(m.msg.AltBody())
}

func (m automationMessage) Send() error {
	return m.mgr.SendCampaignMessage(m.msg)
}

// automationDeps wires the automation worker to the core, the manager and the brand guard.
func automationDeps(co *core.Core, mgr *manager.Manager, brandSvc *brands.Service, lo *log.Logger) automations.Deps {
	return automations.Deps{
		Blocklisted: func(subscriberID int) (bool, error) {
			sub, err := co.GetSubscriber(subscriberID, "", "")
			if err != nil {
				return false, err
			}
			return sub.Status == models.SubscriberStatusBlockListed, nil
		},

		Guard: func(campaignID int) error {
			camp, err := co.GetCampaign(campaignID, "", "")
			if err != nil {
				return err
			}
			listIDs, err := campaignListIDs(camp)
			if err != nil {
				return err
			}
			return brandSvc.GuardSender(listIDs, camp.Messenger, camp.FromEmail, campaignTemplateRef(camp))
		},

		Render: func(campaignID, subscriberID int) (automations.Message, error) {
			camp, err := co.GetCampaign(campaignID, "", "")
			if err != nil {
				return nil, err
			}
			sub, err := co.GetSubscriber(subscriberID, "", "")
			if err != nil {
				return nil, err
			}
			// get-campaign has no media_id column; read the ids from the media JSON
			// so that attachments are sent as in a broadcast.
			var media []struct {
				ID *int64 `json:"id"`
			}
			if len(camp.Media) > 0 {
				if err := json.Unmarshal(camp.Media, &media); err != nil {
					return nil, fmt.Errorf("error reading campaign media: %w", err)
				}
			}
			for _, m := range media {
				if m.ID != nil {
					camp.MediaIDs = append(camp.MediaIDs, *m.ID)
				}
			}

			if err := mgr.LoadInlineImages(&camp); err != nil {
				return nil, fmt.Errorf("error loading inline images: %w", err)
			}
			if err := camp.CompileTemplate(mgr.TemplateFuncs(&camp)); err != nil {
				return nil, fmt.Errorf("error compiling template: %w", err)
			}
			msg, err := mgr.NewCampaignMessage(&camp, sub)
			if err != nil {
				return nil, fmt.Errorf("error rendering message: %w", err)
			}
			return automationMessage{mgr: mgr, msg: msg}, nil
		},
	}
}

// campaignListIDs returns the list ids of a campaign from its lists JSON.
func campaignListIDs(camp models.Campaign) ([]int, error) {
	var lists []struct {
		ID int `json:"id"`
	}
	if len(camp.Lists) > 0 {
		if err := json.Unmarshal(camp.Lists, &lists); err != nil {
			return nil, fmt.Errorf("error reading campaign lists: %w", err)
		}
	}
	ids := make([]int, 0, len(lists))
	for _, l := range lists {
		if l.ID > 0 {
			ids = append(ids, l.ID)
		}
	}
	return ids, nil
}
