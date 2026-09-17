package core

import (
	"os"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

// TestSubscriberNameQueries checks every query that writes subscriber names
// against a real Postgres (see TestDashboardGrowthSQL for LISTMONK_TEST_DSN).
// subscribers.name is a generated column, so these queries must write
// first_name and last_name only, and must save blanks as blanks.
func TestSubscriberNameQueries(t *testing.T) {
	dsn := os.Getenv("LISTMONK_TEST_DSN")
	if dsn == "" {
		t.Skip("LISTMONK_TEST_DSN not set")
	}

	db, q := newTestDB(t, dsn, "lm_names_test", "")

	var (
		noInts  = pq.Array([]int{})
		noUUIDs = pq.Array([]string{})
	)

	insert := func(t *testing.T, email, first, last string) int {
		t.Helper()
		var id int
		if err := q.InsertSubscriber.Get(&id, uuid.Must(uuid.NewV4()).String(), email, first,
			models.SubscriberStatusEnabled, models.JSON{}, noInts, noUUIDs,
			models.SubscriptionStatusUnconfirmed, last); err != nil {
			t.Fatalf("insert-subscriber %s: %v", email, err)
		}
		return id
	}

	t.Run("insert-subscriber", func(t *testing.T) {
		insert(t, "ins@names.test", "Mary", "Jo Smith")
		wantNames(t, db, "ins@names.test", "Mary", "Jo Smith", "Mary Jo Smith")

		insert(t, "blank@names.test", "", "")
		wantNames(t, db, "blank@names.test", "", "", "")
	})

	t.Run("update-subscriber saves blanks", func(t *testing.T) {
		id := insert(t, "upd@names.test", "Ann", "Lee")

		if _, err := q.UpdateSubscriber.Exec(id, "", "", "", "", ""); err != nil {
			t.Fatalf("update-subscriber: %v", err)
		}
		wantNames(t, db, "upd@names.test", "", "", "")

		if _, err := q.UpdateSubscriber.Exec(id, "", "Bo", "", "", ""); err != nil {
			t.Fatalf("update-subscriber: %v", err)
		}
		wantNames(t, db, "upd@names.test", "Bo", "", "Bo")
	})

	t.Run("update-subscriber-with-lists", func(t *testing.T) {
		id := insert(t, "updl@names.test", "Ann", "Lee")

		if _, err := q.UpdateSubscriberWithLists.Exec(id, "", "Cy", "", "", noInts, noUUIDs,
			models.SubscriptionStatusUnconfirmed, false, noInts, false, "Young"); err != nil {
			t.Fatalf("update-subscriber-with-lists: %v", err)
		}
		wantNames(t, db, "updl@names.test", "Cy", "Young", "Cy Young")

		if _, err := q.UpdateSubscriberWithLists.Exec(id, "", "", "", "", noInts, noUUIDs,
			models.SubscriptionStatusUnconfirmed, false, noInts, false, ""); err != nil {
			t.Fatalf("update-subscriber-with-lists: %v", err)
		}
		wantNames(t, db, "updl@names.test", "", "", "")
	})

	t.Run("upsert-subscriber", func(t *testing.T) {
		upsert := func(first, last string, overwrite bool) {
			t.Helper()
			if _, err := q.UpsertSubscriber.Exec(uuid.Must(uuid.NewV4()), "ups@names.test", first, models.JSON{},
				pq.Int64Array{}, models.SubscriptionStatusUnconfirmed, overwrite, true, last); err != nil {
				t.Fatalf("upsert-subscriber: %v", err)
			}
		}

		upsert("Dee", "Dunn", true)
		wantNames(t, db, "ups@names.test", "Dee", "Dunn", "Dee Dunn")

		upsert("Eve", "Ell", true)
		wantNames(t, db, "ups@names.test", "Eve", "Ell", "Eve Ell")

		upsert("Zed", "Zz", false)
		wantNames(t, db, "ups@names.test", "Eve", "Ell", "Eve Ell")
	})

	t.Run("upsert-blocklist-subscriber", func(t *testing.T) {
		if _, err := q.UpsertBlocklistSubscriber.Exec(uuid.Must(uuid.NewV4()), "blk@names.test", "Fay",
			models.JSON{}, "Fox"); err != nil {
			t.Fatalf("upsert-blocklist-subscriber: %v", err)
		}
		wantNames(t, db, "blk@names.test", "Fay", "Fox", "Fay Fox")
	})

	t.Run("name is generated", func(t *testing.T) {
		if _, err := db.Exec(`UPDATE subscribers SET name = 'x' WHERE email = 'ins@names.test'`); err == nil {
			t.Error("writing subscribers.name succeeded; want an error from the generated column")
		}
	})
}

func wantNames(t *testing.T, db *sqlx.DB, email, first, last, name string) {
	t.Helper()

	var gotFirst, gotLast, gotName string
	if err := db.QueryRow(`SELECT first_name, last_name, name FROM subscribers WHERE email = $1`, email).
		Scan(&gotFirst, &gotLast, &gotName); err != nil {
		t.Fatalf("read %s: %v", email, err)
	}
	if gotFirst != first || gotLast != last || gotName != name {
		t.Errorf("%s: got %q / %q / %q, want %q / %q / %q", email, gotFirst, gotLast, gotName, first, last, name)
	}
}
