package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V6_2_1 is a fork migration that adds the campaign_unsubs table for
// attributing unsubscribes to the campaign that caused them.
func V6_2_1(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS campaign_unsubs (
			id               BIGSERIAL PRIMARY KEY,
			campaign_id      INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,

			-- Subscribers may be deleted, but the unsubscribe counts should remain.
			subscriber_id    INTEGER NULL REFERENCES subscribers(id) ON DELETE SET NULL ON UPDATE CASCADE,
			created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);
	`); err != nil {
		return err
	}

	if _, err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_camp_unsubs_camp_sub ON campaign_unsubs(campaign_id, subscriber_id);
	`); err != nil {
		return err
	}

	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_camp_unsubs_date ON campaign_unsubs(created_at);
	`); err != nil {
		return err
	}

	return nil
}
