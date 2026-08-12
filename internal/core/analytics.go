package core

import (
	"net/http"

	"github.com/jmoiron/sqlx/types"
	"github.com/labstack/echo/v4"
)

// GetAnalyticsGrowth returns weekly subscriber growth for a list.
// Pass listID 0 to include every list.
func (c *Core) GetAnalyticsGrowth(listID int) (types.JSONText, error) {
	var out types.JSONText
	if err := c.q.GetAnalyticsGrowth.Get(&out, listID); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "analytics growth", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetAnalyticsCampaigns returns per-campaign opens, clicks and bounces.
func (c *Core) GetAnalyticsCampaigns() (types.JSONText, error) {
	var out types.JSONText
	if err := c.q.GetAnalyticsCampaigns.Get(&out); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "analytics campaigns", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetAnalyticsCohorts returns engagement cohorts for a list.
// Pass listID 0 to include every list.
func (c *Core) GetAnalyticsCohorts(listID int) (types.JSONText, error) {
	var out types.JSONText
	if err := c.q.GetAnalyticsCohorts.Get(&out, listID); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "analytics cohorts", "error", pqErrMsg(err)))
	}

	return out, nil
}
