// Package automations adds welcome-series automations (hcm fork).
// Spec: docs/superpowers/specs/2026-10-12-automations-design.md
package automations

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

//go:embed schema.sql
var schemaSQL string

var (
	ErrEnabledOnList  = errors.New("another automation on this list is enabled")
	ErrEnabled        = errors.New("disable the automation first")
	ErrHasEnrollments = errors.New("the list cannot change after the first enrollment")
)

const oneEnabledConstraint = "automations_one_enabled_per_list"

type Automation struct {
	ID        int       `db:"id" json:"id"`
	Name      string    `db:"name" json:"name"`
	ListID    int       `db:"list_id" json:"list_id"`
	Enabled   bool      `db:"enabled" json:"enabled"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

type Step struct {
	AutomationID int `db:"automation_id" json:"automation_id"`
	Position     int `db:"position" json:"position"`
	DelayMinutes int `db:"delay_minutes" json:"delay_minutes"`
	CampaignID   int `db:"campaign_id" json:"campaign_id"`
}

type Summary struct {
	Automation
	ListName string `db:"list_name" json:"list_name"`
	Enrolled int    `db:"enrolled" json:"enrolled"`
	Sent     int    `db:"sent" json:"sent"`
	Active   int    `db:"active" json:"active"`
	Failed   int    `db:"failed" json:"failed"`
	Unknown  int    `db:"unknown" json:"unknown"`
}

type StepOwnerInfo struct {
	AutomationID int  `db:"automation_id"`
	ListID       int  `db:"list_id"`
	Position     int  `db:"position"`
	Enabled      bool `db:"enabled"`
}

// Bootstrap creates the automations tables. It needs brands.Bootstrap to run
// first (schema ext) and runs before queries are prepared.
func Bootstrap(db *sqlx.DB) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schemaSQL); err != nil {
		return fmt.Errorf("automations bootstrap: %w", err)
	}
	return tx.Commit()
}

type Store struct{ db *sqlx.DB }

func NewStore(db *sqlx.DB) *Store { return &Store{db: db} }

// Create inserts a disabled automation and its step 1 (delay 0) in one transaction.
func (s *Store) Create(name string, listID, campaignID int) (Automation, error) {
	var a Automation
	tx, err := s.db.Beginx()
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	if err := tx.Get(&a, `INSERT INTO ext.automations (name, list_id) VALUES ($1, $2) RETURNING *`, name, listID); err != nil {
		return a, err
	}
	if _, err := tx.Exec(`INSERT INTO ext.automation_steps (automation_id, position, delay_minutes, campaign_id) VALUES ($1, 1, 0, $2)`,
		a.ID, campaignID); err != nil {
		return a, err
	}
	return a, tx.Commit()
}

// Get returns the automation and its steps by position. sql.ErrNoRows when missing.
func (s *Store) Get(id int) (Automation, []Step, error) {
	var a Automation
	if err := s.db.Get(&a, `SELECT * FROM ext.automations WHERE id=$1`, id); err != nil {
		return a, nil, err
	}
	steps := []Step{}
	if err := s.db.Select(&steps, `SELECT * FROM ext.automation_steps WHERE automation_id=$1 ORDER BY position`, id); err != nil {
		return a, nil, err
	}
	return a, steps, nil
}

// All lists automations with counters. listIDs == nil returns all; a non-nil
// slice returns only those lists (an empty slice returns none).
func (s *Store) All(listIDs []int) ([]Summary, error) {
	var filter interface{}
	if listIDs != nil {
		filter = pq.Array(listIDs)
	}
	out := []Summary{}
	err := s.db.Select(&out, `
SELECT a.*, l.name AS list_name,
  (SELECT COUNT(*) FROM ext.automation_enrollments e WHERE e.automation_id = a.id) AS enrolled,
  (SELECT COUNT(*) FROM ext.automation_sends s JOIN ext.automation_enrollments e ON e.id = s.enrollment_id WHERE e.automation_id = a.id) AS sent,
  (SELECT COUNT(*) FROM ext.automation_enrollments e WHERE e.automation_id = a.id AND e.status IN ('active','sending')) AS active,
  (SELECT COUNT(*) FROM ext.automation_enrollments e WHERE e.automation_id = a.id AND e.status = 'failed') AS failed,
  (SELECT COUNT(*) FROM ext.automation_enrollments e WHERE e.automation_id = a.id AND e.status = 'unknown') AS unknown
FROM ext.automations a JOIN lists l ON l.id = a.list_id
WHERE ($1::INT[] IS NULL OR a.list_id = ANY($1::INT[]))
ORDER BY a.id`, filter)
	return out, err
}

// Update changes the name, the list and the delay of existing step positions.
// The list cannot change once an enrollment exists.
func (s *Store) Update(id int, name string, listID int, steps []Step) error {
	tx, err := s.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var cur int
	if err := tx.Get(&cur, `SELECT list_id FROM ext.automations WHERE id=$1 FOR UPDATE`, id); err != nil {
		return err
	}
	if cur != listID {
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM ext.automation_enrollments WHERE automation_id=$1`, id); err != nil {
			return err
		}
		if n > 0 {
			return ErrHasEnrollments
		}
	}
	if _, err := tx.Exec(`UPDATE ext.automations SET name=$2, list_id=$3, updated_at=now() WHERE id=$1`, id, name, listID); err != nil {
		return mapEnabledErr(err)
	}
	for _, st := range steps {
		if _, err := tx.Exec(`UPDATE ext.automation_steps SET delay_minutes=$3 WHERE automation_id=$1 AND position=$2`,
			id, st.Position, st.DelayMinutes); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetEnabled turns an automation on or off. sql.ErrNoRows when missing.
func (s *Store) SetEnabled(id int, on bool) error {
	res, err := s.db.Exec(`UPDATE ext.automations SET enabled=$2, updated_at=now() WHERE id=$1`, id, on)
	if err != nil {
		return mapEnabledErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Delete removes a disabled automation (cascading to steps, enrollments and
// sends) and returns its step campaign ids. The caller deletes the campaigns
// after this commit.
func (s *Store) Delete(id int) ([]int, error) {
	tx, err := s.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var enabled bool
	if err := tx.Get(&enabled, `SELECT enabled FROM ext.automations WHERE id=$1 FOR UPDATE`, id); err != nil {
		return nil, err
	}
	if enabled {
		return nil, ErrEnabled
	}
	ids := []int{}
	if err := tx.Select(&ids, `SELECT campaign_id FROM ext.automation_steps WHERE automation_id=$1 ORDER BY position`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM ext.automations WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

// StepOwner reports which automation step, if any, uses the campaign.
func (s *Store) StepOwner(campaignID int) (StepOwnerInfo, bool, error) {
	var info StepOwnerInfo
	err := s.db.Get(&info, `
SELECT s.automation_id, a.list_id, s.position, a.enabled
FROM ext.automation_steps s JOIN ext.automations a ON a.id = s.automation_id
WHERE s.campaign_id = $1`, campaignID)
	if errors.Is(err, sql.ErrNoRows) {
		return info, false, nil
	}
	return info, err == nil, err
}

// SentByCampaign returns the number of automation sends of the step campaign.
func (s *Store) SentByCampaign(campaignID int) (int, error) {
	var n int
	err := s.db.Get(&n, `SELECT COUNT(*) FROM ext.automation_sends WHERE campaign_id=$1`, campaignID)
	return n, err
}

func mapEnabledErr(err error) error {
	var pe *pq.Error
	if errors.As(err, &pe) && pe.Constraint == oneEnabledConstraint {
		return ErrEnabledOnList
	}
	return err
}
