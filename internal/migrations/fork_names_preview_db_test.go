package migrations

import (
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// These tests need a Postgres server where the user may create databases:
//
//	LISTMONK_TEST_DSN='postgres://lmtest@127.0.0.1:55432/lmtest?sslmode=disable' \
//	  go test ./internal/migrations/ -run Fork -v

var discard = log.New(io.Discard, "", 0)

func forkTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	dsn := os.Getenv("LISTMONK_TEST_DSN")
	if dsn == "" {
		t.Skip("LISTMONK_TEST_DSN not set")
	}

	admin, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close() })

	name := fmt.Sprintf("lm_fork_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create database: %v", err)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + name

	db, err := sqlx.Connect("postgres", u.String())
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(func() {
		db.Close()
		admin.Exec("DROP DATABASE IF EXISTS " + name)
	})

	schema, err := os.ReadFile("../../schema.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("install schema: %v", err)
	}

	return db
}

func mustExec(t *testing.T, db *sqlx.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// toPreForkSchema reshapes a schema.sql install into the shape production has
// before the fork step (gunmade f525ea3), whether or not schema.sql already
// carries the fork columns.
func toPreForkSchema(t *testing.T, db *sqlx.DB) {
	t.Helper()

	names, preview, err := forkColumns(db)
	if err != nil {
		t.Fatalf("fork columns: %v", err)
	}
	if names {
		mustExec(t, db, `ALTER TABLE subscribers DROP COLUMN name`)
		mustExec(t, db, `ALTER TABLE subscribers DROP COLUMN first_name, DROP COLUMN last_name`)
		mustExec(t, db, `ALTER TABLE subscribers ADD COLUMN name TEXT NOT NULL DEFAULT ''`)
	}
	if preview {
		mustExec(t, db, `ALTER TABLE campaigns DROP COLUMN preview_text`)
	}
}

type forkNameCase struct {
	email, stored          string
	first, last, generated string
}

var forkNameCases = []forkNameCase{
	// Invented by the importer / admin API rule (models.FallbackName).
	{"bkirkpatrick00@example.com", "Bkirkpatrick00", "", "", ""},
	{"john.smith@example.com", "John Smith", "", "", ""},
	{"john_doe@example.com", "John_doe", "", "", ""},
	// Invented by the public subscription form rule (raw local part).
	{"pop.up@example.com", "pop.up", "", "", ""},
	// Supplied by a person.
	{"jsmith@example.com", "Johnny S.", "Johnny", "S.", "Johnny S."},
	{"bkirk00@example.com", "Brady Kirkpatrick", "Brady", "Kirkpatrick", "Brady Kirkpatrick"},
	{"mary@example.com", "Mary Jo Van Der Berg", "Mary", "Jo Van Der Berg", "Mary Jo Van Der Berg"},
	{"c@example.com", "Cher", "Cher", "", "Cher"},
	{"pat@example.com", "  Pat   O'Brien ", "Pat", "O'Brien", "Pat O'Brien"},
	{"empty@example.com", "", "", "", ""},
}

func seedForkNames(t *testing.T, db *sqlx.DB) {
	t.Helper()
	for _, c := range forkNameCases {
		mustExec(t, db, `INSERT INTO subscribers (uuid, email, name) VALUES (gen_random_uuid(), $1, $2)`, c.email, c.stored)
	}
}

func snapshotNames(t *testing.T, db *sqlx.DB) string {
	t.Helper()
	var s string
	if err := db.Get(&s, `SELECT COALESCE(string_agg(id || ':' || first_name || '|' || last_name || '|' || name, ',' ORDER BY id), '') FROM subscribers`); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return s
}

func TestRunForkMigratesPreForkDatabase(t *testing.T) {
	// Exercise several write batches with only a handful of rows.
	orig := forkBatchSize
	forkBatchSize = 3
	t.Cleanup(func() { forkBatchSize = orig })

	db := forkTestDB(t)
	toPreForkSchema(t, db)
	seedForkNames(t, db)

	if pending, err := ForkPending(db); err != nil || !pending {
		t.Fatalf("ForkPending before = %v, %v; want true, nil", pending, err)
	}
	if err := RunFork(db, discard); err != nil {
		t.Fatalf("RunFork: %v", err)
	}
	if pending, err := ForkPending(db); err != nil || pending {
		t.Fatalf("ForkPending after = %v, %v; want false, nil", pending, err)
	}

	for _, c := range forkNameCases {
		var first, last, name string
		if err := db.QueryRow(`SELECT first_name, last_name, name FROM subscribers WHERE email = $1`, c.email).Scan(&first, &last, &name); err != nil {
			t.Fatalf("%s: %v", c.email, err)
		}
		if first != c.first || last != c.last || name != c.generated {
			t.Errorf("%s (%q): got %q / %q / %q, want %q / %q / %q",
				c.email, c.stored, first, last, name, c.first, c.last, c.generated)
		}
	}

	// name is derived now: a direct write must fail loudly.
	if _, err := db.Exec(`UPDATE subscribers SET name = 'x' WHERE email = 'mary@example.com'`); err == nil {
		t.Error("writing subscribers.name succeeded; want an error from the generated column")
	}

	// Campaigns gained preview_text with an empty default.
	var preview string
	if err := db.QueryRow(`INSERT INTO campaigns (uuid, name, subject, from_email, body, messenger)
		VALUES (gen_random_uuid(), 'c', 's', 'from@example.com', 'b', 'email') RETURNING preview_text`).Scan(&preview); err != nil {
		t.Fatalf("insert campaign: %v", err)
	}
	if preview != "" {
		t.Errorf("preview_text default = %q, want empty", preview)
	}
}

func TestRunForkIsIdempotent(t *testing.T) {
	db := forkTestDB(t)
	toPreForkSchema(t, db)
	seedForkNames(t, db)

	if err := RunFork(db, discard); err != nil {
		t.Fatalf("first RunFork: %v", err)
	}
	before := snapshotNames(t, db)

	if err := RunFork(db, discard); err != nil {
		t.Fatalf("second RunFork: %v", err)
	}
	if after := snapshotNames(t, db); after != before {
		t.Errorf("second RunFork changed data:\nbefore %s\nafter  %s", before, after)
	}
}

func TestRunForkRefusesPartialSchema(t *testing.T) {
	db := forkTestDB(t)
	toPreForkSchema(t, db)
	mustExec(t, db, `ALTER TABLE subscribers ADD COLUMN first_name TEXT NOT NULL DEFAULT ''`)

	if err := RunFork(db, discard); err == nil {
		t.Fatal("RunFork on a partial schema succeeded; want an error")
	}
	if pending, err := ForkPending(db); err != nil || !pending {
		t.Errorf("ForkPending on a partial schema = %v, %v; want true, nil", pending, err)
	}
}

// A fresh install gets the fork columns from schema.sql; the step must no-op.
func TestForkNoOpOnFreshInstall(t *testing.T) {
	db := forkTestDB(t)

	if pending, err := ForkPending(db); err != nil || pending {
		t.Fatalf("ForkPending on schema.sql = %v, %v; want false, nil", pending, err)
	}
	if err := RunFork(db, discard); err != nil {
		t.Fatalf("RunFork on schema.sql: %v", err)
	}
}
