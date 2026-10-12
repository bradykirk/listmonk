package main

import (
	"net/http"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/automations"
	"github.com/knadh/listmonk/internal/brands"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
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
