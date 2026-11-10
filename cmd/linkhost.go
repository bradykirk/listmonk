package main

import (
	"net/http"

	"github.com/knadh/listmonk/internal/brands"
	"github.com/labstack/echo/v4"
)

// FORK(hcm): per-brand link host. Plan:
// docs/superpowers/plans/2026-11-10-brand-link-host.md. Logic and tests are in
// internal/brands/linkcheck.go.

// linkChecks holds this process's live link host check tokens.
var linkChecks = brands.NewLinkChecks()

var linkCheckClient = brands.LinkCheckClient(nil)

// LinkHostCheck handles GET /ext/link-host-check/:token (public). It answers
// only a token that a brand save in this process started.
func (a *App) LinkHostCheck(c echo.Context) error {
	resp, ok := linkChecks.Answer(c.Param("token"))
	if !ok {
		return c.NoContent(http.StatusNotFound)
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.String(http.StatusOK, resp)
}

// applyLinkHost applies the request's link_host to b (400 on refusal).
func applyLinkHost(b *brands.Brand, req *string, prev string) error {
	verify := func(host string) error { return linkChecks.Verify(host, linkCheckClient) }
	if err := brands.ApplyLinkHost(b, req, prev, verify); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return nil
}
