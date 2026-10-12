package main

import (
	"encoding/json"
	"net/http"

	"github.com/knadh/listmonk/internal/brands"
	"github.com/knadh/listmonk/models"
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

// filterExportables drops the items of data that are not in exportables.
func filterExportables(data *models.SubscriberExportProfile, exportables map[string]bool) {
	if _, ok := exportables["profile"]; !ok {
		data.Profile = nil
	}
	if _, ok := exportables["subscriptions"]; !ok {
		data.Subscriptions = nil
	}
	if _, ok := exportables["campaign_views"]; !ok {
		data.CampaignViews = nil
	}
	if _, ok := exportables["link_clicks"]; !ok {
		data.LinkClicks = nil
	}
}

// exportBrandSubscriberData is exportSubscriberData limited to one brand's
// lists and campaigns. The profile is shared and does not change.
func (a *App) exportBrandSubscriberData(id int, brandListIDs []int, exportables map[string]bool) ([]byte, error) {
	if brandListIDs == nil {
		// A nil array is SQL NULL, which would match nothing in ANY() but is unclear; keep it explicit.
		brandListIDs = []int{}
	}
	var data models.SubscriberExportProfile
	if err := a.queries.ExtExportSubscriberData.Get(&data, id, nil, pq.Array(brandListIDs)); err != nil {
		return nil, err
	}
	filterExportables(&data, exportables)
	return json.MarshalIndent(data, "", "  ")
}
