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

// GetAnalyticsCampaignTimeline returns hourly opens and clicks for the first
// 48 hours after a campaign started.
func (c *Core) GetAnalyticsCampaignTimeline(campID int) (types.JSONText, error) {
	var out types.JSONText
	if err := c.q.GetAnalyticsCampaignTimeline.Get(&out, campID); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "campaign timeline", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetAnalyticsCampaignLinks returns the most clicked links in a campaign.
func (c *Core) GetAnalyticsCampaignLinks(campID int) (types.JSONText, error) {
	var out types.JSONText
	if err := c.q.GetAnalyticsCampaignLinks.Get(&out, campID); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "campaign links", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetAnalyticsActivity returns recent per-subscriber opens and clicks.
// Pass campID 0 for every campaign.
func (c *Core) GetAnalyticsActivity(campID int) (types.JSONText, error) {
	var out types.JSONText
	if err := c.q.GetAnalyticsActivity.Get(&out, campID); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "activity", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetAnalyticsSummary returns the headline counts for the KPI row.
// Pass listID 0 to include every list.
func (c *Core) GetAnalyticsSummary(listID int) (types.JSONText, error) {
	var out types.JSONText
	if err := c.q.GetAnalyticsSummary.Get(&out, listID); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "analytics summary", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetAnalyticsDomains returns engagement and bounces per mailbox provider.
// Pass listID 0 to include every list.
func (c *Core) GetAnalyticsDomains(listID int) (types.JSONText, error) {
	var out types.JSONText
	if err := c.q.GetAnalyticsDomains.Get(&out, listID); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "domain analytics", "error", pqErrMsg(err)))
	}

	return out, nil
}

// GetAnalyticsSendTimes returns open rates by the weekday and hour of sending.
func (c *Core) GetAnalyticsSendTimes() (types.JSONText, error) {
	var out types.JSONText
	if err := c.q.GetAnalyticsSendTimes.Get(&out); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "send times", "error", pqErrMsg(err)))
	}

	return out, nil
}
