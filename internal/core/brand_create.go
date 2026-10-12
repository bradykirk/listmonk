package core

import (
	"net/http"

	"github.com/gofrs/uuid/v5"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	null "gopkg.in/volatiletech/null.v6"
)

// FORK(hcm): brand-aware helpers. Spec: docs/superpowers/specs/2026-10-11-brands-design.md

// FORK(hcm): create a template and its ext ownership row in one transaction,
// so a failed ownership insert leaves no unassigned row.
func (c *Core) CreateTemplateOwned(name, typ, subject string, body []byte, bodySource null.String, owner *string, assign bool) (models.Template, error) {
	tx, err := c.db.Beginx()
	if err != nil {
		return models.Template{}, err
	}
	defer tx.Rollback()

	var id int
	if err := tx.Stmtx(c.q.CreateTemplate).Get(&id, name, typ, subject, body, bodySource); err != nil {
		return models.Template{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.template}", "error", pqErrMsg(err)))
	}
	if assign {
		if _, err := tx.Exec(`INSERT INTO ext.template_brands (template_id, brand_slug) VALUES ($1, $2)`, id, owner); err != nil {
			return models.Template{}, echo.NewHTTPError(http.StatusInternalServerError, "error assigning the template brand: "+pqErrMsg(err))
		}
	}
	if err := tx.Commit(); err != nil {
		return models.Template{}, err
	}

	return c.GetTemplate(id, false)
}

// FORK(hcm): insert a media file and its ext ownership row in one
// transaction. The caller deletes the stored files on error.
func (c *Core) InsertMediaOwned(fileName, thumbName, contentType string, meta models.JSON, provider string, s media.Store, owner *string, assign bool) (media.Media, error) {
	uu, err := uuid.NewV4()
	if err != nil {
		c.log.Printf("error generating UUID: %v", err)
		return media.Media{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUUID", "error", err.Error()))
	}

	tx, err := c.db.Beginx()
	if err != nil {
		return media.Media{}, err
	}
	defer tx.Rollback()

	var newID int
	if err := tx.Stmtx(c.q.InsertMedia).Get(&newID, uu, fileName, thumbName, contentType, provider, meta); err != nil {
		c.log.Printf("error inserting uploaded file to db: %v", err)
		return media.Media{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorCreating", "name", "{globals.terms.media}", "error", pqErrMsg(err)))
	}
	if assign {
		if _, err := tx.Exec(`INSERT INTO ext.media_brands (media_id, brand_slug) VALUES ($1, $2)`, newID, owner); err != nil {
			return media.Media{}, echo.NewHTTPError(http.StatusInternalServerError, "error assigning the media brand: "+pqErrMsg(err))
		}
	}
	if err := tx.Commit(); err != nil {
		return media.Media{}, err
	}

	return c.GetMedia(newID, "", "", s)
}
