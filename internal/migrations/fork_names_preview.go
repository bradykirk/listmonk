package migrations

import (
	"fmt"
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

// Gunmade fork schema step: subscriber first/last names and campaign preview
// text. Spec: docs/superpowers/specs/2026-09-17-subscriber-names-preview-text-design.md.
//
// This step is deliberately NOT an entry in cmd/upgrade.go's migList. migList
// runs every entry whose semver is above the last recorded version, so a fork
// version number would make a later upstream migration with the same or a
// lower number skip silently. Instead, cmd/upgrade.go calls RunFork on every
// --upgrade and ForkPending on every normal start. Both calls must stay ahead
// of the "nothing pending" early returns in that file: a database already at
// the last migList version takes those paths.

// forkBatchSize is the number of subscribers written per UPDATE. A var so the
// tests can exercise several batches with a few rows.
var forkBatchSize = 5000

// forkColumns reports which of the step's two marker columns exist.
func forkColumns(q sqlx.Queryer) (names, preview bool, err error) {
	const exists = `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2)`

	if err = sqlx.Get(q, &names, exists, "subscribers", "first_name"); err != nil {
		return false, false, err
	}
	if err = sqlx.Get(q, &preview, exists, "campaigns", "preview_text"); err != nil {
		return false, false, err
	}

	return names, preview, nil
}

// ForkPending reports whether the fork step has not been applied.
func ForkPending(db *sqlx.DB) (bool, error) {
	names, preview, err := forkColumns(db)
	if err != nil {
		return false, err
	}

	return !names || !preview, nil
}

// RunFork applies the fork step in one transaction. It is a no-op when both
// marker columns exist, and refuses to guess when only one does.
func RunFork(db *sqlx.DB, lo *log.Logger) error {
	names, preview, err := forkColumns(db)
	if err != nil {
		return fmt.Errorf("checking fork columns: %w", err)
	}
	if names && preview {
		return nil
	}
	if names != preview {
		return fmt.Errorf("partial fork schema (subscribers.first_name=%v, campaigns.preview_text=%v); restore the database from backup",
			names, preview)
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`ALTER TABLE subscribers
		ADD COLUMN first_name TEXT NOT NULL DEFAULT '',
		ADD COLUMN last_name  TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("adding name columns: %w", err)
	}

	// Read everything first: lib/pq cannot run an UPDATE on the connection
	// while a result set is still open.
	var subs []struct {
		ID    int    `db:"id"`
		Email string `db:"email"`
		Name  string `db:"name"`
	}
	if err := tx.Select(&subs, `SELECT id, email, name FROM subscribers ORDER BY id`); err != nil {
		return fmt.Errorf("reading subscribers: %w", err)
	}

	var (
		ids     []int64
		firsts  []string
		lasts   []string
		blanked int
		split   int
	)
	flush := func() error {
		if len(ids) == 0 {
			return nil
		}
		if _, err := tx.Exec(`UPDATE subscribers s SET first_name = u.first_name, last_name = u.last_name
			FROM UNNEST($1::INT[], $2::TEXT[], $3::TEXT[]) AS u(id, first_name, last_name)
			WHERE s.id = u.id`, pq.Array(ids), pq.Array(firsts), pq.Array(lasts)); err != nil {
			return fmt.Errorf("writing names: %w", err)
		}
		ids, firsts, lasts = ids[:0], firsts[:0], lasts[:0]
		return nil
	}

	for _, s := range subs {
		if models.IsFallbackName(s.Name, s.Email) {
			// The new columns already default to ''. Log the cleared name here,
			// before the old `name` column is dropped below, so it stays
			// recoverable from the deploy log and not only from the pre-deploy
			// backup.
			lo.Printf("gunmade fork migration blanking name: id=%d email=%s name=%q", s.ID, s.Email, s.Name)
			blanked++
			continue
		}

		first, last := models.SplitName(s.Name)
		if first == "" && last == "" {
			continue
		}

		split++
		ids = append(ids, int64(s.ID))
		firsts = append(firsts, first)
		lasts = append(lasts, last)
		if len(ids) >= forkBatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}

	for _, q := range []string{
		`ALTER TABLE subscribers DROP COLUMN name`,
		// Keep this expression identical to schema.sql.
		`ALTER TABLE subscribers ADD COLUMN name TEXT NOT NULL GENERATED ALWAYS AS (btrim(first_name || ' ' || last_name)) STORED`,
		`ALTER TABLE campaigns ADD COLUMN preview_text TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	lo.Printf("gunmade fork migration applied: %d subscribers, %d fallback names blanked, %d names split into first/last",
		len(subs), blanked, split)

	return nil
}
