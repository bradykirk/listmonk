package main

import (
	"net/http"

	"github.com/knadh/listmonk/internal/brands"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// FORK(hcm): brand helpers for handlers.

// campaignInBrand returns 404 when a brand is selected and campaign id is not in it.
func (a *App) campaignInBrand(c echo.Context, id int) error {
	_, ids, ok := brands.BrandFrom(c)
	if !ok {
		return nil
	}
	if ids == nil {
		// A nil array is SQL NULL, which would match every campaign.
		ids = []int{}
	}
	var in bool
	if err := a.queries.ExtCampaignInBrand.Get(&in, id, pq.Array(ids)); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error checking campaign brand")
	}
	if !in {
		return echo.NewHTTPError(http.StatusNotFound, a.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.campaign}"))
	}
	return nil
}

// brandListsOrNil returns the selected brand's list IDs, or nil for All brands.
func brandListsOrNil(c echo.Context) []int {
	_, ids, ok := brands.BrandFrom(c)
	if !ok {
		return nil
	}
	if ids == nil {
		// A selected brand with no lists must match nothing, not everything.
		return []int{}
	}
	return ids
}

// listsInBrand returns 400 when a brand is selected and any of listIDs is outside it.
func (a *App) listsInBrand(c echo.Context, listIDs []int) error {
	_, ids, ok := brands.BrandFrom(c)
	if !ok {
		return nil
	}
	allowed := make(map[int]bool, len(ids))
	for _, id := range ids {
		allowed[id] = true
	}
	for _, id := range listIDs {
		if !allowed[id] {
			return echo.NewHTTPError(http.StatusBadRequest, "All lists must belong to the selected brand.")
		}
	}
	return nil
}
