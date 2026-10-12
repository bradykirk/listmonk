package automations

import (
	"database/sql"
	"errors"
	"time"
)

// maxAttempts is the attempt number whose failure ends the enrollment.
const maxAttempts = 5

// Claimed is an enrollment taken by the worker for one send attempt.
type Claimed struct {
	ID           int64 `db:"id"`
	AutomationID int   `db:"automation_id"`
	SubscriberID int   `db:"subscriber_id"`
	Position     int   `db:"position"`
	Attempts     int   `db:"attempts"`
	ListID       int   `db:"list_id"`
	CampaignID   int   `db:"campaign_id"`
	Enabled      bool  `db:"enabled"`
	HasStep      bool  `db:"has_step"`
}

const claimSQL = `
WITH c AS (
  UPDATE ext.automation_enrollments e
     SET status = 'sending', sending_at = now(), attempts = attempts + 1, updated_at = now()
   WHERE e.id = (SELECT id FROM ext.automation_enrollments
                  WHERE status = 'active' AND next_at <= now()
                  ORDER BY next_at LIMIT 1 FOR UPDATE SKIP LOCKED)
  RETURNING e.id, e.automation_id, e.subscriber_id, e.next_position, e.attempts
)
SELECT c.id, c.automation_id, c.subscriber_id, c.next_position AS position, c.attempts,
       COALESCE(a.list_id, 0) AS list_id, COALESCE(a.enabled, false) AS enabled,
       COALESCE(st.campaign_id, 0) AS campaign_id, (st.campaign_id IS NOT NULL) AS has_step
FROM c LEFT JOIN ext.automations a ON a.id = c.automation_id
       LEFT JOIN ext.automation_steps st ON st.automation_id = c.automation_id AND st.position = c.next_position`

// ClaimOne moves one due enrollment to 'sending'. It returns nil, nil when none is due.
func (s *Store) ClaimOne() (*Claimed, error) {
	var c Claimed
	if err := s.db.Get(&c, claimSQL); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// Finish sets a final state (skipped or failed) with the reason.
func (s *Store) Finish(id int64, status, reason string) error {
	_, err := s.db.Exec(`UPDATE ext.automation_enrollments
		SET status = $2, next_at = NULL, sending_at = NULL, last_error = $3, updated_at = now()
		WHERE id = $1`, id, status, reason)
	return err
}

// Advance records the send and moves the enrollment to the next step, or to done.
func (s *Store) Advance(c *Claimed) error {
	tx, err := s.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO ext.automation_sends (enrollment_id, position, campaign_id)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, c.ID, c.Position, c.CampaignID); err != nil {
		return err
	}

	var delay int
	err = tx.Get(&delay, `SELECT delay_minutes FROM ext.automation_steps
		WHERE automation_id = $1 AND position = $2`, c.AutomationID, c.Position+1)
	switch {
	case err == nil:
		_, err = tx.Exec(`UPDATE ext.automation_enrollments
			SET status = 'active', next_position = next_position + 1,
			    next_at = now() + make_interval(mins => $2),
			    attempts = 0, last_error = NULL, sending_at = NULL, updated_at = now()
			WHERE id = $1`, c.ID, delay)
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.Exec(`UPDATE ext.automation_enrollments
			SET status = 'done', next_at = NULL, sending_at = NULL, updated_at = now()
			WHERE id = $1`, c.ID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Retry schedules another attempt with backoff, or fails the enrollment on the last attempt.
func (s *Store) Retry(c *Claimed, cause string) error {
	if c.Attempts >= maxAttempts {
		return s.Finish(c.ID, "failed", cause)
	}
	_, err := s.db.Exec(`UPDATE ext.automation_enrollments
		SET status = 'active', next_at = now() + make_interval(secs => $2),
		    last_error = $3, sending_at = NULL, updated_at = now()
		WHERE id = $1`, c.ID, Backoff(c.Attempts).Seconds(), cause)
	return err
}

// Defer returns the enrollment to 'active' without using an attempt.
func (s *Store) Defer(c *Claimed, d time.Duration, reason string) error {
	_, err := s.db.Exec(`UPDATE ext.automation_enrollments
		SET status = 'active', attempts = attempts - 1,
		    next_at = now() + make_interval(secs => $2),
		    last_error = $3, sending_at = NULL, updated_at = now()
		WHERE id = $1`, c.ID, d.Seconds(), reason)
	return err
}

// MarkStale moves enrollments stuck in 'sending' for longer than after to 'unknown'.
func (s *Store) MarkStale(after time.Duration) (int, error) {
	res, err := s.db.Exec(`UPDATE ext.automation_enrollments
		SET status = 'unknown', next_at = NULL, updated_at = now()
		WHERE status = 'sending' AND sending_at < now() - make_interval(secs => $1)`, after.Seconds())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// Backoff is the wait after a failed attempt number.
func Backoff(attempts int) time.Duration {
	switch attempts {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 30 * time.Minute
	}
	return 2 * time.Hour
}
