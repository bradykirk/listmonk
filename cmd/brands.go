package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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

// templateInBrand returns 404 when a brand is selected and template id is
// neither the brand's own nor shared.
func (a *App) templateInBrand(c echo.Context, id int) error {
	b, _, ok := brands.BrandFrom(c)
	if !ok {
		return nil
	}
	in, err := a.brands.Store.TemplateVisible(id, b.Slug)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error checking template brand")
	}
	if !in {
		return echo.NewHTTPError(http.StatusNotFound, a.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.template}"))
	}
	return nil
}

// campaignRefsInBrand checks the template, archive template and media of a
// campaign request against the selected brand. It is a no-op for All brands.
func (a *App) campaignRefsInBrand(c echo.Context, o campReq) error {
	if _, _, ok := brands.BrandFrom(c); !ok {
		return nil
	}
	check := func(err error) error {
		if err == nil {
			return nil
		}
		if he, ok := err.(*echo.HTTPError); ok && he.Code == http.StatusNotFound {
			return echo.NewHTTPError(http.StatusBadRequest, "The template and media must belong to the selected brand.")
		}
		return err
	}
	if o.TemplateID.Valid && o.TemplateID.Int != 0 {
		if err := check(a.templateInBrand(c, o.TemplateID.Int)); err != nil {
			return err
		}
	}
	if o.ArchiveTemplateID.Valid && o.ArchiveTemplateID.Int != 0 {
		if err := check(a.templateInBrand(c, o.ArchiveTemplateID.Int)); err != nil {
			return err
		}
	}
	for _, id := range o.MediaIDs {
		if err := check(a.mediaInBrand(c, id)); err != nil {
			return err
		}
	}
	return nil
}

// defaultTemplateInBrand validates, in a brand view, the default template that a
// campaign with no template (template_id absent, null or 0) uses. Visual campaigns
// never use it. No-op for All brands.
func (a *App) defaultTemplateInBrand(c echo.Context, o campReq) error {
	b, _, ok := brands.BrandFrom(c)
	if !ok || (o.TemplateID.Valid && o.TemplateID.Int != 0) || o.ContentType == models.CampaignContentTypeVisual {
		return nil
	}
	id, err := a.brands.Store.DefaultTemplateID()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error checking the default template")
	}
	if id == 0 {
		return nil
	}
	in, err := a.brands.Store.TemplateVisible(id, b.Slug)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error checking template brand")
	}
	if !in {
		return echo.NewHTTPError(http.StatusBadRequest, "The default template does not belong to the selected brand. Choose a template.")
	}
	return nil
}

// mediaInBrand is templateInBrand for media.
func (a *App) mediaInBrand(c echo.Context, id int) error {
	b, _, ok := brands.BrandFrom(c)
	if !ok {
		return nil
	}
	in, err := a.brands.Store.MediaVisible(id, b.Slug)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error checking media brand")
	}
	if !in {
		return echo.NewHTTPError(http.StatusNotFound, a.i18n.Ts("globals.messages.notFound", "name", "{globals.terms.media}"))
	}
	return nil
}

// brandSlugOrNil returns the selected brand's slug, or nil for All brands.
func brandSlugOrNil(c echo.Context) *string {
	b, _, ok := brands.BrandFrom(c)
	if !ok {
		return nil
	}
	return &b.Slug
}

// createOwner returns the ext owner of a new template or media file (spec 4.3, 5.6).
// In a brand view it is the selected brand and the request cannot choose another.
// In All brands it is the optional ?brand= parameter: a slug, or "shared" for a
// shared item. Absent means no owner row (unassigned). assign is false only then.
func (a *App) createOwner(c echo.Context) (*string, bool, error) {
	if b, _, ok := brands.BrandFrom(c); ok {
		slug := b.Slug
		return &slug, true, nil
	}
	v := strings.TrimSpace(c.QueryParam("brand"))
	switch v {
	case "":
		return nil, false, nil
	case brands.SharedOwner:
		return nil, true, nil
	}
	if _, ok, err := a.brands.Store.Get(v); err != nil {
		return nil, false, echo.NewHTTPError(http.StatusInternalServerError, "error loading brand")
	} else if !ok {
		return nil, false, echo.NewHTTPError(http.StatusBadRequest, "unknown brand")
	}
	return &v, true, nil
}

// brandWithCount is a brand with the number of lists it owns.
type brandWithCount struct {
	brands.Brand
	ListCount int `json:"list_count"`
}

// GetBrands handles GET /api/ext/brands.
func (a *App) GetBrands(c echo.Context) error {
	all, err := a.brands.Store.All()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading brands")
	}
	tags, err := a.brands.Store.ListTags()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading lists")
	}
	out := make([]brandWithCount, 0, len(all))
	for _, b := range all {
		out = append(out, brandWithCount{Brand: b, ListCount: len(brands.BrandListIDs(b.Slug, tags, all))})
	}
	return c.JSON(http.StatusOK, okResp{out})
}

// CreateBrand handles POST /api/ext/brands.
func (a *App) CreateBrand(c echo.Context) error {
	var b brands.Brand
	if err := c.Bind(&b); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidData")+": "+err.Error())
	}
	b.Slug = strings.TrimSpace(b.Slug)
	if err := brands.Validate(&b); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if _, ok, err := a.brands.Store.Get(b.Slug); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading brand")
	} else if ok {
		return echo.NewHTTPError(http.StatusConflict, "brand already exists")
	}
	return a.saveBrand(c, b)
}

// UpdateBrand handles PUT /api/ext/brands/:slug. The slug cannot change.
func (a *App) UpdateBrand(c echo.Context) error {
	slug := c.Param("slug")
	var b brands.Brand
	if err := c.Bind(&b); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidData")+": "+err.Error())
	}
	if b.Slug != "" && b.Slug != slug {
		return echo.NewHTTPError(http.StatusBadRequest, "the brand slug cannot change")
	}
	b.Slug = slug
	if err := brands.Validate(&b); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if _, ok, err := a.brands.Store.Get(slug); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading brand")
	} else if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "unknown brand")
	}
	return a.saveBrand(c, b)
}

func (a *App) saveBrand(c echo.Context, b brands.Brand) error {
	if err := a.brands.Store.Upsert(b); err != nil {
		a.log.Printf("error saving brand %s: %v", b.Slug, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "error saving brand")
	}
	out, _, err := a.brands.Store.Get(b.Slug)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading brand")
	}
	return c.JSON(http.StatusOK, okResp{out})
}

// DeleteBrand handles DELETE /api/ext/brands/:slug. A brand that owns a list,
// template, or media file cannot be deleted (409).
func (a *App) DeleteBrand(c echo.Context) error {
	slug := c.Param("slug")
	if _, ok, err := a.brands.Store.Get(slug); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading brand")
	} else if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "unknown brand")
	}
	if err := a.brands.Store.Delete(slug); err != nil {
		if errors.Is(err, brands.ErrBrandInUse) {
			return echo.NewHTTPError(http.StatusConflict, err.Error())
		}
		a.log.Printf("error deleting brand %s: %v", slug, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "error deleting brand")
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// readOwnerBody reads {"brand": "slug"} or {"brand": null} (shared). The
// field is required, so an empty body cannot make an item shared by mistake.
func (a *App) readOwnerBody(c echo.Context) (*string, error) {
	var body map[string]json.RawMessage
	if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidData")+": "+err.Error())
	}
	raw, ok := body["brand"]
	if !ok {
		return nil, echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.missingFields", "name", "brand"))
	}
	var slug *string
	if err := json.Unmarshal(raw, &slug); err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidData")+": brand")
	}
	if slug == nil {
		return nil, nil
	}
	if _, ok, err := a.brands.Store.Get(*slug); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "error loading brand")
	} else if !ok {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "unknown brand")
	}
	return slug, nil
}

// SetTemplateBrand handles PUT /api/ext/templates/:id/brand.
func (a *App) SetTemplateBrand(c echo.Context) error {
	id := getID(c)
	slug, err := a.readOwnerBody(c)
	if err != nil {
		return err
	}
	if _, err := a.core.GetTemplate(id, true); err != nil {
		return err
	}
	if err := a.brands.Store.SetTemplateBrand(id, slug); err != nil {
		a.log.Printf("error setting template %d brand: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "error setting the template brand")
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// SetMediaBrand handles PUT /api/ext/media/:id/brand.
func (a *App) SetMediaBrand(c echo.Context) error {
	id := getID(c)
	slug, err := a.readOwnerBody(c)
	if err != nil {
		return err
	}
	if _, err := a.core.GetMedia(id, "", "", a.media); err != nil {
		return err
	}
	if err := a.brands.Store.SetMediaBrand(id, slug); err != nil {
		a.log.Printf("error setting media %d brand: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "error setting the media brand")
	}
	return c.JSON(http.StatusOK, okResp{true})
}

// listOwner is a list's brand for the "All brands" labels.
type listOwner struct {
	Slug      string `json:"slug"`
	Ambiguous bool   `json:"ambiguous"`
}

// GetOwnership handles GET /api/ext/ownership: the owner of every template,
// media file, and list. A template or media ID that is absent is unassigned;
// a null owner is shared. A list with an empty slug is unassigned unless
// ambiguous.
func (a *App) GetOwnership(c echo.Context) error {
	tpls, err := a.brands.Store.TemplateOwners()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading template owners")
	}
	med, err := a.brands.Store.MediaOwners()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading media owners")
	}
	all, err := a.brands.Store.All()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading brands")
	}
	tags, err := a.brands.Store.ListTags()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "error loading lists")
	}
	lists := make(map[int]listOwner, len(tags))
	for id, t := range tags {
		lb := brands.BrandOfList(t, all)
		lists[id] = listOwner{Slug: lb.Slug, Ambiguous: lb.Ambiguous}
	}
	return c.JSON(http.StatusOK, okResp{map[string]any{
		"templates": tpls,
		"media":     med,
		"lists":     lists,
	}})
}
