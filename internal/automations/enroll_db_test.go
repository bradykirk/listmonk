package automations

import (
	"fmt"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func setupEnabled(t *testing.T, db *sqlx.DB, optin string) (*Store, int, int) {
	s := NewStore(db)
	l := mkList(t, db, "TA", optin, "ta")
	a, err := s.Create("a", l, mkCampaign(t, db, l))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled(a.ID, true); err != nil {
		t.Fatal(err)
	}
	return s, l, a.ID
}

func TestEnrollRules(t *testing.T) {
	db := testDB(t)
	s, l, _ := setupEnabled(t, db, "single")
	other := mkList(t, db, "RI", "single", "ri")
	cases := []struct {
		name, st string
		list     int
		want     bool
		reason   string
	}{
		{"confirmed", "confirmed", l, true, ""},
		{"single unconfirmed is active", "unconfirmed", l, true, ""},
		{"unsubscribed", "unsubscribed", l, false, ReasonNotActive},
		{"no automation on list", "confirmed", other, false, ReasonNoAutomation},
	}
	for i, c := range cases {
		sub := mkSub(t, db, fmt.Sprintf("e%d@example.com", i), c.list, c.st)
		ok, reason, err := s.Enroll(sub, c.list)
		if err != nil || ok != c.want || reason != c.reason {
			t.Errorf("%s: %v %q %v", c.name, ok, reason, err)
		}
	}
	sub := mkSub(t, db, "bl@example.com", l, "confirmed")
	db.MustExec(`UPDATE subscribers SET status='blocklisted' WHERE id=$1`, sub)
	if ok, reason, _ := s.Enroll(sub, l); ok || reason != ReasonBlocklisted {
		t.Errorf("blocklisted: %v %q", ok, reason)
	}
	if ok, reason, _ := s.Enroll(999999, l); ok || reason != ReasonNoSubscriber {
		t.Errorf("missing: %v %q", ok, reason)
	}
}

func TestEnrollDoubleOptinUnconfirmed(t *testing.T) {
	db := testDB(t)
	s, l, _ := setupEnabled(t, db, "double")
	sub := mkSub(t, db, "d@example.com", l, "unconfirmed")
	if ok, reason, _ := s.Enroll(sub, l); ok || reason != ReasonNotActive {
		t.Fatalf("%v %q", ok, reason)
	}
}

func TestEnrollTwice(t *testing.T) {
	db := testDB(t)
	s, l, aid := setupEnabled(t, db, "single")
	sub := mkSub(t, db, "t@example.com", l, "confirmed")
	if ok, _, err := s.Enroll(sub, l); !ok || err != nil {
		t.Fatal("first enroll")
	}
	if ok, reason, _ := s.Enroll(sub, l); ok || reason != ReasonAlreadyEnrolled {
		t.Fatalf("second: %v %q", ok, reason)
	}
	var n int
	db.Get(&n, `SELECT COUNT(*) FROM ext.automation_enrollments WHERE automation_id=$1`, aid)
	if n != 1 {
		t.Fatalf("enrollments = %d", n)
	}
	var due bool
	db.Get(&due, `SELECT next_at <= now() + interval '1 second' FROM ext.automation_enrollments WHERE automation_id=$1`, aid)
	if !due {
		t.Fatal("delay 0 must be due now")
	}
}

func TestIntentNewSignup(t *testing.T) {
	db := testDB(t)
	s, l, _ := setupEnabled(t, db, "single")
	ids, err := s.BeginIntents(" New@Example.com ", []int{l})
	if err != nil || len(ids) != 1 {
		t.Fatalf("begin: %v %v", ids, err)
	}
	mkSub(t, db, "new@example.com", l, "unconfirmed") // the upstream insert
	n, err := s.ResolveIntents(ids)
	if err != nil || n != 1 {
		t.Fatalf("resolve: %d %v", n, err)
	}
	var left int
	db.Get(&left, `SELECT COUNT(*) FROM ext.automation_intents`)
	if left != 0 {
		t.Fatal("resolved intent must be deleted")
	}
}

func TestIntentAlreadyActiveNoWelcome(t *testing.T) {
	db := testDB(t)
	s, l, _ := setupEnabled(t, db, "single")
	mkSub(t, db, "old@example.com", l, "confirmed")
	ids, _ := s.BeginIntents("old@example.com", []int{l})
	if n, _ := s.ResolveIntents(ids); n != 0 {
		t.Fatal("a re-submit by an active subscriber must not enroll")
	}
}

func TestIntentSweepRecoversCrash(t *testing.T) {
	db := testDB(t)
	s, l, _ := setupEnabled(t, db, "single")
	ids, _ := s.BeginIntents("crash@example.com", []int{l})
	mkSub(t, db, "crash@example.com", l, "unconfirmed")
	db.MustExec(`UPDATE ext.automation_intents SET created_at = now() - interval '3 minutes' WHERE id = $1`, ids[0])
	n, err := s.SweepIntents(2*time.Minute, 7*24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("sweep: %d %v", n, err)
	}
}

func TestIntentPendingOptinKeptThenExpired(t *testing.T) {
	db := testDB(t)
	s, l, _ := setupEnabled(t, db, "double")
	ids, _ := s.BeginIntents("p@example.com", []int{l})
	mkSub(t, db, "p@example.com", l, "unconfirmed")
	db.MustExec(`UPDATE ext.automation_intents SET created_at = now() - interval '1 day' WHERE id = $1`, ids[0])
	if n, _ := s.SweepIntents(2*time.Minute, 7*24*time.Hour); n != 0 {
		t.Fatal("not active yet")
	}
	var left int
	db.Get(&left, `SELECT COUNT(*) FROM ext.automation_intents`)
	if left != 1 {
		t.Fatal("a pending double opt-in intent must be kept")
	}
	db.MustExec(`UPDATE ext.automation_intents SET created_at = now() - interval '8 days'`)
	s.SweepIntents(2*time.Minute, 7*24*time.Hour)
	db.Get(&left, `SELECT COUNT(*) FROM ext.automation_intents`)
	if left != 0 {
		t.Fatal("an intent older than 7 days must be deleted")
	}
}
