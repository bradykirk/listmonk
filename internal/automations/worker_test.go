package automations

import (
	"errors"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
)

type fakeMsg struct {
	content string
	err     error
	sent    *int
}

func (m fakeMsg) Content() string { return m.content }
func (m fakeMsg) Send() error {
	if m.err == nil {
		*m.sent++
	}
	return m.err
}

func depsWith(sent *int, content string, sendErr, guardErr error) Deps {
	return Deps{
		Blocklisted: func(int) (bool, error) { return false, nil },
		Guard:       func(int) error { return guardErr },
		Render:      func(int, int) (Message, error) { return fakeMsg{content, sendErr, sent}, nil },
	}
}

func status(t *testing.T, db *sqlx.DB) string {
	var s string
	if err := db.Get(&s, `SELECT status FROM ext.automation_enrollments`); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWorkerSendsOnce(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	sent := 0
	w := NewWorker(s, depsWith(&sent, "Welcome", nil, nil), log.New(io.Discard, "", 0))
	w.Tick()
	w.Tick()
	if sent != 1 || status(t, db) != "done" {
		t.Fatalf("sent %d status %s", sent, status(t, db))
	}
}

func TestWorkerRetryThenFail(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	sent := 0
	w := NewWorker(s, depsWith(&sent, "Welcome", errors.New("smtp down"), nil), log.New(io.Discard, "", 0))
	for i := 0; i < 5; i++ {
		db.MustExec(`UPDATE ext.automation_enrollments SET next_at = now() - interval '1 second' WHERE status='active'`)
		w.Tick()
	}
	var sends int
	db.Get(&sends, `SELECT COUNT(*) FROM ext.automation_sends`)
	if sent != 0 || sends != 0 || status(t, db) != "failed" {
		t.Fatalf("sent %d sends %d status %s", sent, sends, status(t, db))
	}
}

func TestWorkerGuardSkips(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	sent := 0
	w := NewWorker(s, depsWith(&sent, "Welcome", nil, errors.New("messenger not allowed")), log.New(io.Discard, "", 0))
	w.Tick()
	if sent != 0 || status(t, db) != "skipped" {
		t.Fatal(status(t, db))
	}
}

func TestWorkerMarkerDefers(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	sent := 0
	w := NewWorker(s, depsWith(&sent, "Hi\n<p>[[SENDER_INFO]]</p>\n", nil, nil), log.New(io.Discard, "", 0))
	w.Tick()
	var later bool
	db.Get(&later, `SELECT next_at > now() + interval '9 minutes' FROM ext.automation_enrollments`)
	if sent != 0 || status(t, db) != "active" || !later {
		t.Fatalf("sent %d status %s", sent, status(t, db))
	}
}

func TestWorkerSkipsUnsubscribedAndDisabled(t *testing.T) {
	db, s, aid, sub := claimSetup(t)
	db.MustExec(`UPDATE subscriber_lists SET status='unsubscribed' WHERE subscriber_id=$1`, sub)
	sent := 0
	w := NewWorker(s, depsWith(&sent, "Welcome", nil, nil), log.New(io.Discard, "", 0))
	w.Tick()
	if sent != 0 || status(t, db) != "skipped" {
		t.Fatal("unsubscribed must skip")
	}

	// A disabled automation also skips.
	db2, s2, aid2, _ := claimSetup(t)
	db2.MustExec(`UPDATE ext.automations SET enabled=false WHERE id=$1`, aid2)
	w2 := NewWorker(s2, depsWith(&sent, "Welcome", nil, nil), log.New(io.Discard, "", 0))
	w2.Tick()
	if sent != 0 || status(t, db2) != "skipped" {
		t.Fatal("disabled must skip")
	}
	_ = aid
}

func TestWorkerBlocklistedSkips(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	sent := 0
	d := depsWith(&sent, "Welcome", nil, nil)
	d.Blocklisted = func(int) (bool, error) { return true, nil }
	NewWorker(s, d, log.New(io.Discard, "", 0)).Tick()
	if sent != 0 || status(t, db) != "skipped" {
		t.Fatal(status(t, db))
	}
}

func TestWorkerRecoversFromPanic(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	var l int
	db.Get(&l, `SELECT list_id FROM ext.automations LIMIT 1`)
	second := mkSub(t, db, "w2@example.com", l, "confirmed")
	if ok, _, _ := s.Enroll(second, l); !ok {
		t.Fatal("enroll second")
	}
	sent := 0
	d := depsWith(&sent, "Welcome", nil, nil)
	calls := 0
	d.Render = func(int, int) (Message, error) {
		calls++
		if calls == 1 {
			panic("boom")
		}
		return fakeMsg{"Welcome", nil, &sent}, nil
	}
	var buf strings.Builder
	w := NewWorker(s, d, log.New(&buf, "", 0))
	w.Tick() // must return
	if sent != 1 {
		t.Fatalf("the second enrollment must still be sent, sent %d", sent)
	}
	var sending, done int
	db.Get(&sending, `SELECT COUNT(*) FROM ext.automation_enrollments WHERE status='sending'`)
	db.Get(&done, `SELECT COUNT(*) FROM ext.automation_enrollments WHERE status='done'`)
	if sending != 1 || done != 1 {
		t.Fatalf("sending %d done %d", sending, done)
	}
	if !strings.Contains(buf.String(), "automations: panic in step") || !strings.Contains(buf.String(), "boom") {
		t.Fatalf("log: %q", buf.String())
	}
}
