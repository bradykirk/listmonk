package main

import (
	"net/http"
	"time"

	"github.com/knadh/listmonk/internal/core"
	"github.com/labstack/echo/v4"
)

// GetDashboardGrowth returns signups, unsubscribes and audience size over the
// requested range for the Dashboard's audience growth section (gunmade fork).
// Query parameters: range (7d, 30d, 90d, 12m) and tz (IANA time zone name).
func (a *App) GetDashboardGrowth(c echo.Context) error {
	r, err := core.ParseGrowthRange(c.QueryParam("range"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	tz, err := core.ParseGrowthTZ(c.QueryParam("tz"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	out, err := a.core.GetDashboardGrowth(tz, r, time.Now())
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}
