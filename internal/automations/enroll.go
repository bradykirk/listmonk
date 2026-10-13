package automations

import (
	"database/sql"
	"errors"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// Enroll outcomes (spec 6.1).
const (
	ReasonNoAutomation    = "no-automation"
	ReasonNoSubscriber    = "no-subscriber"
	ReasonBlocklisted     = "blocklisted"
	ReasonNotActive       = "not-active"
	ReasonAlreadyEnrolled = "already-enrolled"
	ReasonNoStep          = "no-step"
)

// activeSQL is the spec 6.0 active-subscription test. No row = not active.
const activeSQL = `
SELECT (sl.status = 'confirmed' OR (sl.status = 'unconfirmed' AND l.optin = 'single'))
FROM subscriber_lists sl JOIN lists l ON l.id = sl.list_id
WHERE sl.subscriber_id = $1 AND sl.list_id = $2`

type queryer interface {
	Get(dest interface{}, query string, args ...interface{}) error
}

func activeOn(q queryer, subscriberID, listID int) (bool, error) {
	var active bool
	err := q.Get(&active, activeSQL, subscriberID, listID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return active, err
}

// ActiveOn reports whether the subscriber has an active subscription on the list.
func (s *Store) ActiveOn(subscriberID, listID int) (bool, error) {
	return activeOn(s.db, subscriberID, listID)
}

// Enroll enrolls the subscriber in the enabled automation of the list.
// reason is empty when enrolled is true.
func (s *Store) Enroll(subscriberID, listID int) (enrolled bool, reason string, err error) {
	tx, err := s.db.Beginx()
	if err != nil {
		return false, "", err
	}
	defer tx.Rollback()
	enrolled, reason, err = enrollTx(tx, subscriberID, listID)
	if err != nil {
		return false, "", err
	}
	return enrolled, reason, tx.Commit()
}

func enrollTx(tx *sqlx.Tx, subscriberID, listID int) (bool, string, error) {
	var autoID int
	if err := tx.Get(&autoID, `SELECT id FROM ext.automations WHERE list_id=$1 AND enabled`, listID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ReasonNoAutomation, nil
		}
		return false, "", err
	}

	var status string
	if err := tx.Get(&status, `SELECT status FROM subscribers WHERE id=$1`, subscriberID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ReasonNoSubscriber, nil
		}
		return false, "", err
	}
	if status == "blocklisted" {
		return false, ReasonBlocklisted, nil
	}

	active, err := activeOn(tx, subscriberID, listID)
	if err != nil {
		return false, "", err
	}
	if !active {
		return false, ReasonNotActive, nil
	}

	var delay int
	if err := tx.Get(&delay, `SELECT delay_minutes FROM ext.automation_steps WHERE automation_id=$1 AND position=1`, autoID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ReasonNoStep, nil
		}
		return false, "", err
	}

	res, err := tx.Exec(`
INSERT INTO ext.automation_enrollments (automation_id, subscriber_id, next_position, next_at, status)
VALUES ($1, $2, 1, now() + make_interval(mins => $3), 'active')
ON CONFLICT (automation_id, subscriber_id) DO NOTHING`, autoID, subscriberID, delay)
	if err != nil {
		return false, "", err
	}
	if n, err := res.RowsAffected(); err != nil {
		return false, "", err
	} else if n == 0 {
		return false, ReasonAlreadyEnrolled, nil
	}
	return true, "", nil
}

// BeginIntents records one intent row per list that has an enabled automation,
// before the upstream public-form write. It returns the new intent ids.
func (s *Store) BeginIntents(email string, listIDs []int) ([]int64, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	var subID int
	known := true
	if err := s.db.Get(&subID, `SELECT id FROM subscribers WHERE LOWER(email)=$1`, email); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		known = false
	}

	ids := []int64{}
	for _, listID := range listIDs {
		var autoID int
		if err := s.db.Get(&autoID, `SELECT id FROM ext.automations WHERE list_id=$1 AND enabled`, listID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return ids, err
		}
		was := false
		if known {
			var err error
			if was, err = activeOn(s.db, subID, listID); err != nil {
				return ids, err
			}
		}
		var id int64
		if err := s.db.Get(&id, `INSERT INTO ext.automation_intents (email, list_id, was_active) VALUES ($1, $2, $3) RETURNING id`,
			email, listID, was); err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

type intentRow struct {
	Email     string `db:"email"`
	ListID    int    `db:"list_id"`
	WasActive bool   `db:"was_active"`
}

// resolveOne settles one intent. It returns 1 when it enrolled the subscriber.
// A subscriber who is not active yet keeps the row (pending double opt-in).
func (s *Store) resolveOne(id int64) (int, error) {
	tx, err := s.db.Beginx()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var in intentRow
	if err := tx.Get(&in, `SELECT email, list_id, was_active FROM ext.automation_intents WHERE id=$1 FOR UPDATE SKIP LOCKED`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}

	var subID int
	if err := tx.Get(&subID, `SELECT id FROM subscribers WHERE LOWER(email)=$1`, in.Email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, tx.Commit()
		}
		return 0, err
	}
	active, err := activeOn(tx, subID, in.ListID)
	if err != nil {
		return 0, err
	}
	if !active {
		return 0, tx.Commit()
	}

	n := 0
	if !in.WasActive {
		ok, _, err := enrollTx(tx, subID, in.ListID)
		if err != nil {
			return 0, err
		}
		if ok {
			n = 1
		}
	}
	if _, err := tx.Exec(`DELETE FROM ext.automation_intents WHERE id=$1`, id); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// ResolveIntents settles the intents after the upstream write. It returns the number enrolled.
func (s *Store) ResolveIntents(ids []int64) (int, error) {
	total := 0
	for _, id := range ids {
		n, err := s.resolveOne(id)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// sweepBatch is the most intents one SweepIntents call resolves.
const sweepBatch = 100

// SweepIntents deletes intents older than maxAge, then resolves at most sweepBatch of those
// older than minAge. Intents never swept go first, then the least recently swept, so
// pending rows cannot starve newer ones. One failing intent does not stop the others;
// the first error is returned after the loop.
func (s *Store) SweepIntents(minAge, maxAge time.Duration) (enrolled int, err error) {
	if _, err := s.db.Exec(`DELETE FROM ext.automation_intents WHERE created_at < now() - make_interval(secs => $1)`,
		maxAge.Seconds()); err != nil {
		return 0, err
	}
	var ids []int64
	if err := s.db.Select(&ids, `UPDATE ext.automation_intents SET swept_at = clock_timestamp()
		WHERE id IN (SELECT id FROM ext.automation_intents WHERE created_at < now() - make_interval(secs => $1)
			ORDER BY swept_at NULLS FIRST, id LIMIT $2)
		RETURNING id`,
		minAge.Seconds(), sweepBatch); err != nil {
		return 0, err
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var first error
	for _, id := range ids {
		n, err := s.resolveOne(id)
		if err != nil {
			log.Printf("automations: resolve intent %d: %v", id, err)
			if first == nil {
				first = err
			}
			continue
		}
		enrolled += n
	}
	return enrolled, first
}

// ListIDsByUUID maps list UUIDs to list ids. Unknown UUIDs are skipped.
func (s *Store) ListIDsByUUID(uuids []string) ([]int, error) {
	ids := []int{}
	err := s.db.Select(&ids, `SELECT id FROM lists WHERE uuid = ANY($1::UUID[])`, pq.Array(uuids))
	return ids, err
}
