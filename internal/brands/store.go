// Package brands adds brand scoping and the cross-brand send guard (hcm fork).
// Spec: docs/superpowers/specs/2026-10-11-brands-design.md
package brands

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

//go:embed schema.sql
var schemaSQL string

type Brand struct {
	Slug        string         `db:"slug" json:"slug"`
	Name        string         `db:"name" json:"name"`
	ListTags    pq.StringArray `db:"list_tags" json:"list_tags"`
	Messengers  pq.StringArray `db:"messengers" json:"messengers"`
	FromDomains pq.StringArray `db:"from_domains" json:"from_domains"`
	SortOrder   int            `db:"sort_order" json:"sort_order"`
}

// Bootstrap creates the ext schema. It runs before queries are prepared,
// because queries in queries/*.sql reference ext tables.
func Bootstrap(db *sqlx.DB) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schemaSQL); err != nil {
		return fmt.Errorf("ext bootstrap: %w", err)
	}
	return tx.Commit()
}

// SeedInitial inserts the first brands (spec 4.4) when ext.brands is empty.
// It never changes existing rows, so admin edits survive restarts.
func SeedInitial(db *sqlx.DB) (bool, error) {
	tx, err := db.Beginx()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// Lock so two instances that start together cannot both seed.
	if _, err := tx.Exec(`LOCK TABLE ext.brands IN EXCLUSIVE MODE`); err != nil {
		return false, err
	}
	var n int
	if err := tx.Get(&n, `SELECT COUNT(*) FROM ext.brands`); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if _, err := tx.Exec(`
		INSERT INTO ext.brands (slug, name, list_tags, messengers, from_domains, sort_order) VALUES
		  ('ri', 'RangeInsiders',    '{ri,ri-cold}', '{email-ri}', '{rangeinsiders.com}', 1),
		  ('ta', 'Tactical Advisor', '{ta}',         '{email-ta}', '{news.tacticaladvisor.net}', 2),
		  ('ct', 'CT',               '{ct}',         '{email-ct}', '{}', 3)`); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`
		INSERT INTO ext.template_brands (template_id, brand_slug)
		SELECT v.id, v.slug FROM (VALUES (1, NULL), (2, NULL), (3, NULL), (4, NULL), (5, 'ri'), (6, 'ri')) AS v(id, slug)
		WHERE EXISTS (SELECT 1 FROM templates t WHERE t.id = v.id)
		ON CONFLICT (template_id) DO NOTHING`); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

type Store struct{ db *sqlx.DB }

func NewStore(db *sqlx.DB) *Store { return &Store{db: db} }

func (s *Store) All() ([]Brand, error) {
	out := []Brand{}
	err := s.db.Select(&out, `SELECT slug, name, list_tags, messengers, from_domains, sort_order FROM ext.brands ORDER BY sort_order, slug`)
	return out, err
}

func (s *Store) Get(slug string) (Brand, bool, error) {
	var b Brand
	err := s.db.Get(&b, `SELECT slug, name, list_tags, messengers, from_domains, sort_order FROM ext.brands WHERE slug = $1`, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return Brand{}, false, nil
	}
	return b, err == nil, err
}

func (s *Store) Upsert(b Brand) error {
	// The array columns are NOT NULL; a nil slice binds as NULL.
	normalize(&b)
	_, err := s.db.Exec(`
		INSERT INTO ext.brands (slug, name, list_tags, messengers, from_domains, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (slug) DO UPDATE SET name = $2, list_tags = $3, messengers = $4,
		  from_domains = $5, sort_order = $6, updated_at = now()`,
		b.Slug, b.Name, b.ListTags, b.Messengers, b.FromDomains, b.SortOrder)
	return err
}

// ErrBrandInUse is returned when a brand still owns a list, template, or media file.
var ErrBrandInUse = errors.New("brand still owns lists, templates, or media")

func (s *Store) Delete(slug string) error {
	var used bool
	err := s.db.Get(&used, `
		SELECT EXISTS (SELECT 1 FROM ext.template_brands WHERE brand_slug = $1)
		    OR EXISTS (SELECT 1 FROM ext.media_brands WHERE brand_slug = $1)
		    OR EXISTS (SELECT 1 FROM lists l, ext.brands b
		               WHERE b.slug = $1 AND l.tags::text[] && b.list_tags)`, slug)
	if err != nil {
		return err
	}
	if used {
		return ErrBrandInUse
	}
	_, err = s.db.Exec(`DELETE FROM ext.brands WHERE slug = $1`, slug)
	return err
}

func (s *Store) ListTags() (map[int][]string, error) {
	rows := []struct {
		ID   int            `db:"id"`
		Tags pq.StringArray `db:"tags"`
	}{}
	if err := s.db.Select(&rows, `SELECT id, COALESCE(tags, '{}') AS tags FROM lists`); err != nil {
		return nil, err
	}
	out := make(map[int][]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Tags
	}
	return out, nil
}

func (s *Store) SetTemplateBrand(id int, slug *string) error {
	_, err := s.db.Exec(`INSERT INTO ext.template_brands (template_id, brand_slug) VALUES ($1, $2)
		ON CONFLICT (template_id) DO UPDATE SET brand_slug = $2`, id, slug)
	return err
}

func (s *Store) SetMediaBrand(id int, slug *string) error {
	_, err := s.db.Exec(`INSERT INTO ext.media_brands (media_id, brand_slug) VALUES ($1, $2)
		ON CONFLICT (media_id) DO UPDATE SET brand_slug = $2`, id, slug)
	return err
}

func (s *Store) owners(q string) (map[int]*string, error) {
	rows := []struct {
		ID   int     `db:"id"`
		Slug *string `db:"brand_slug"`
	}{}
	if err := s.db.Select(&rows, q); err != nil {
		return nil, err
	}
	out := make(map[int]*string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Slug
	}
	return out, nil
}

func (s *Store) TemplateOwners() (map[int]*string, error) {
	return s.owners(`SELECT template_id AS id, brand_slug FROM ext.template_brands`)
}

func (s *Store) MediaOwners() (map[int]*string, error) {
	return s.owners(`SELECT media_id AS id, brand_slug FROM ext.media_brands`)
}

// TemplateVisible reports whether template id shows in brand slug: it is the
// brand's own or shared. An unassigned template shows only in All brands.
func (s *Store) TemplateVisible(id int, slug string) (bool, error) {
	var ok bool
	err := s.db.Get(&ok, `SELECT EXISTS (SELECT 1 FROM ext.template_brands
		WHERE template_id = $1 AND (brand_slug IS NULL OR brand_slug = $2))`, id, slug)
	return ok, err
}

// MediaVisible is TemplateVisible for media.
func (s *Store) MediaVisible(id int, slug string) (bool, error) {
	var ok bool
	err := s.db.Get(&ok, `SELECT EXISTS (SELECT 1 FROM ext.media_brands
		WHERE media_id = $1 AND (brand_slug IS NULL OR brand_slug = $2))`, id, slug)
	return ok, err
}

// Visible reports whether item id shows in brand slug, given the owners map
// of TemplateOwners or MediaOwners: the brand's own or shared (nil owner).
func Visible(owners map[int]*string, slug string, id int) bool {
	o, ok := owners[id]
	return ok && (o == nil || *o == slug)
}
