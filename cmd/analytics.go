package main

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

// listIDParam reads an optional list_id query parameter.
// It returns 0 when the parameter is absent or invalid, which means "all lists".
func listIDParam(c echo.Context) int {
	id, err := strconv.Atoi(c.QueryParam("list_id"))
	if err != nil || id < 0 {
		return 0
	}

	return id
}

// GetAnalyticsGrowth returns weekly subscriber growth for the audience chart.
func (a *App) GetAnalyticsGrowth(c echo.Context) error {
	out, err := a.core.GetAnalyticsGrowth(listIDParam(c))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// GetAnalyticsCampaigns returns per-campaign opens, clicks and bounces.
func (a *App) GetAnalyticsCampaigns(c echo.Context) error {
	out, err := a.core.GetAnalyticsCampaigns()
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// GetAnalyticsCohorts returns engagement cohorts for the sunset view.
func (a *App) GetAnalyticsCohorts(c echo.Context) error {
	out, err := a.core.GetAnalyticsCohorts(listIDParam(c))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// campIDParam reads an optional campaign_id query parameter.
// It returns 0 when absent or invalid, which means "all campaigns".
func campIDParam(c echo.Context) int {
	id, err := strconv.Atoi(c.QueryParam("campaign_id"))
	if err != nil || id < 0 {
		return 0
	}

	return id
}

// GetAnalyticsCampaignTimeline returns hourly opens and clicks for a campaign.
func (a *App) GetAnalyticsCampaignTimeline(c echo.Context) error {
	out, err := a.core.GetAnalyticsCampaignTimeline(campIDParam(c))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// GetAnalyticsCampaignLinks returns the most clicked links in a campaign.
func (a *App) GetAnalyticsCampaignLinks(c echo.Context) error {
	out, err := a.core.GetAnalyticsCampaignLinks(campIDParam(c))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// GetAnalyticsActivity returns the recent subscriber activity feed.
func (a *App) GetAnalyticsActivity(c echo.Context) error {
	out, err := a.core.GetAnalyticsActivity(campIDParam(c))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}
