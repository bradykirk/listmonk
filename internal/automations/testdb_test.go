package automations

import (
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/brands"
	"github.com/lib/pq"
)

// testDB creates a throwaway database with the upstream schema.sql installed.
// It skips unless LISTMONK_TEST_DSN is set.
func testDB(t *testing.T) *sqlx.DB {
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

	name := fmt.Sprintf("automations_%d", time.Now().UnixNano())
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
	if err := brands.Bootstrap(db); err != nil {
		t.Fatalf("brands bootstrap: %v", err)
	}
	if err := Bootstrap(db); err != nil {
		t.Fatalf("automations bootstrap: %v", err)
	}
	return db
}

// mkList inserts a list and returns its id. optin is "single" or "double".
func mkList(t *testing.T, db *sqlx.DB, name, optin string, tags ...string) int {
	t.Helper()
	var id int
	if err := db.Get(&id, `INSERT INTO lists (uuid, name, type, optin, tags) VALUES (gen_random_uuid(), $1, 'public', $2, $3) RETURNING id`,
		name, optin, pq.Array(tags)); err != nil {
		t.Fatalf("mkList: %v", err)
	}
	return id
}

// mkSub inserts a subscriber, optionally subscribed to listID with status st ("" = no subscription).
func mkSub(t *testing.T, db *sqlx.DB, email string, listID int, st string) int {
	t.Helper()
	var id int
	if err := db.Get(&id, `INSERT INTO subscribers (uuid, email, first_name, status) VALUES (gen_random_uuid(), $1, $1, 'enabled') RETURNING id`, email); err != nil {
		t.Fatalf("mkSub: %v", err)
	}
	if st != "" {
		if _, err := db.Exec(`INSERT INTO subscriber_lists (subscriber_id, list_id, status) VALUES ($1, $2, $3)`, id, listID, st); err != nil {
			t.Fatalf("mkSub list: %v", err)
		}
	}
	return id
}

// mkCampaign inserts a draft campaign on listID and returns its id.
func mkCampaign(t *testing.T, db *sqlx.DB, listID int) int {
	t.Helper()
	// schema.sql installs no templates (listmonk --install does), so make one.
	if _, err := db.Exec(`INSERT INTO templates (id, name, type, subject, body) VALUES (1, 'tpl', 'campaign', '', '{{ template "content" . }}') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("mkCampaign template: %v", err)
	}
	var id int
	if err := db.Get(&id, `INSERT INTO campaigns (uuid, name, subject, from_email, body, content_type, type, status, messenger, template_id)
		VALUES (gen_random_uuid(), 'step', 'Welcome', 'a@example.com', 'hi', 'html', 'regular', 'draft', 'email', 1) RETURNING id`); err != nil {
		t.Fatalf("mkCampaign: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO campaign_lists (campaign_id, list_id, list_name) VALUES ($1, $2, 'l')`, id, listID); err != nil {
		t.Fatalf("mkCampaign list: %v", err)
	}
	return id
}
