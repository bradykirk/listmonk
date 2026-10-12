package automations

import (
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func claimSetup(t *testing.T) (*sqlx.DB, *Store, int, int) {
	db := testDB(t)
	s, l, aid := setupEnabled(t, db, "single")
	sub := mkSub(t, db, "w@example.com", l, "confirmed")
	if ok, _, _ := s.Enroll(sub, l); !ok {
		t.Fatal("enroll")
	}
	return db, s, aid, sub
}

func TestClaimAdvanceDone(t *testing.T) {
	db, s, aid, _ := claimSetup(t)
	c, err := s.ClaimOne()
	if err != nil || c == nil || c.AutomationID != aid || c.Position != 1 || c.Attempts != 1 || !c.Enabled || !c.HasStep {
		t.Fatalf("claim: %+v %v", c, err)
	}
	if again, _ := s.ClaimOne(); again != nil {
		t.Fatal("a claimed row must not be claimed twice")
	}
	if err := s.Advance(c); err != nil {
		t.Fatal(err)
	}
	var st string
	var sends int
	db.Get(&st, `SELECT status FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	db.Get(&sends, `SELECT COUNT(*) FROM ext.automation_sends WHERE enrollment_id=$1`, c.ID)
	if st != "done" || sends != 1 {
		t.Fatalf("status %s sends %d", st, sends)
	}
}

func TestAdvanceToStepTwo(t *testing.T) {
	db, s, aid, _ := claimSetup(t)
	var l int
	db.Get(&l, `SELECT list_id FROM ext.automations WHERE id=$1`, aid)
	db.MustExec(`INSERT INTO ext.automation_steps (automation_id, position, delay_minutes, campaign_id) VALUES ($1, 2, 1440, $2)`, aid, mkCampaign(t, db, l))
	c, _ := s.ClaimOne()
	s.Advance(c)
	var st string
	var pos int
	var later bool
	db.Get(&st, `SELECT status FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	db.Get(&pos, `SELECT next_position FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	db.Get(&later, `SELECT next_at > now() + interval '23 hours' FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	if st != "active" || pos != 2 || !later {
		t.Fatalf("%s %d %v", st, pos, later)
	}
}

func TestRetryBackoffAndFail(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	want := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
	for i, d := range want {
		if Backoff(i+1) != d {
			t.Fatalf("Backoff(%d) = %v", i+1, Backoff(i+1))
		}
	}
	for i := 1; i <= 5; i++ {
		db.MustExec(`UPDATE ext.automation_enrollments SET next_at = now() - interval '1 second' WHERE status='active'`)
		c, _ := s.ClaimOne()
		if c == nil || c.Attempts != i {
			t.Fatalf("attempt %d: %+v", i, c)
		}
		s.Retry(c, "smtp down")
	}
	var st, le string
	db.Get(&st, `SELECT status FROM ext.automation_enrollments`)
	db.Get(&le, `SELECT last_error FROM ext.automation_enrollments`)
	if st != "failed" || le != "smtp down" {
		t.Fatalf("%s %s", st, le)
	}
}

func TestDeferRefundsAttempt(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	c, _ := s.ClaimOne()
	s.Defer(c, 10*time.Minute, "sender info marker")
	var st string
	var att int
	db.Get(&st, `SELECT status FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	db.Get(&att, `SELECT attempts FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	if st != "active" || att != 0 {
		t.Fatalf("%s %d", st, att)
	}
}

func TestMarkStale(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	c, _ := s.ClaimOne()
	db.MustExec(`UPDATE ext.automation_enrollments SET sending_at = now() - interval '11 minutes' WHERE id=$1`, c.ID)
	if n, _ := s.MarkStale(10 * time.Minute); n != 1 {
		t.Fatal("stale")
	}
	var st string
	db.Get(&st, `SELECT status FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	if st != "unknown" {
		t.Fatal(st)
	}
}
