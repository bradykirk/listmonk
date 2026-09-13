package core

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jmoiron/sqlx/types"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// Audience growth for the home Dashboard (gunmade fork).
//
// The Dashboard shows signups per day (or per week for the 12 month range) and
// the audience size over time, in the viewer's time zone. The numbers are
// computed live from subscribers and subscriber_lists; the counting rules are
// documented on the get-dashboard-growth query in queries/analytics.sql.

// GrowthRange is one of the fixed ranges the Dashboard offers.
type GrowthRange struct {
	Label   string
	Buckets int
	Unit    string
}

// growthRanges maps the range query parameter to its bucket count and size.
// 12 months uses weekly buckets because 365 daily bars are too thin to read.
var growthRanges = map[string]GrowthRange{
	"7d":  {Label: "7d", Buckets: 7, Unit: "day"},
	"30d": {Label: "30d", Buckets: 30, Unit: "day"},
	"90d": {Label: "90d", Buckets: 90, Unit: "day"},
	"12m": {Label: "12m", Buckets: 52, Unit: "week"},
}

// ParseGrowthRange reads the range query parameter. An empty value means 30d.
func ParseGrowthRange(s string) (GrowthRange, error) {
	if s == "" {
		s = "30d"
	}

	r, ok := growthRanges[s]
	if !ok {
		return GrowthRange{}, fmt.Errorf("invalid range %q: use 7d, 30d, 90d or 12m", s)
	}

	return r, nil
}

// ParseGrowthTZ reads the tz query parameter, an IANA time zone name such as
// America/Chicago. An empty value means UTC. "Local" is rejected because it
// would silently mean the server's zone rather than the viewer's.
func ParseGrowthTZ(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "UTC", nil
	}

	if s == "Local" {
		return "", fmt.Errorf("invalid time zone %q", s)
	}

	if _, err := time.LoadLocation(s); err != nil {
		return "", fmt.Errorf("invalid time zone %q", s)
	}

	return s, nil
}

// GetDashboardGrowth returns signups, unsubscribes and audience size per bucket
// of the given range, with day boundaries in the tz time zone.
func (c *Core) GetDashboardGrowth(tz string, r GrowthRange, now time.Time) (types.JSONText, error) {
	var out types.JSONText
	if err := c.q.GetDashboardGrowth.Get(&out, tz, r.Buckets, r.Unit, now, r.Label); err != nil {
		// Go and Postgres ship separate time zone databases. A name that Go
		// accepts but Postgres does not is bad input, not a server fault.
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "22023" {
			return nil, echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid time zone %q", tz))
		}

		return nil, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorFetching", "name", "audience growth", "error", pqErrMsg(err)))
	}

	return out, nil
}
