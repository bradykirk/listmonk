package automations

import (
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/goyesql/v2"
	"github.com/lib/pq"
)

// loadQuery reads a goyesql file, picks the named query and applies the replacements.
func loadQuery(t *testing.T, path, name string, repl map[string]string) string {
	t.Helper()
	qs, err := goyesql.ParseFile(path)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	q, ok := qs[name]
	if !ok {
		t.Fatalf("query %s not found", name)
	}
	out := q.Query
	for k, v := range repl {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}

func TestHiddenCampaignNotListed(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	normal := mkCampaign(t, db, l)
	step := mkCampaign(t, db, l)
	if _, err := s.Create("a", l, step); err != nil {
		t.Fatal(err)
	}
	q := loadQuery(t, "../../queries/campaigns.sql", "query-campaigns", map[string]string{"%order%": "c.created_at DESC"})
	ids := func(id int) []int {
		var out []int
		rows, err := db.Queryx(q, id, pq.StringArray{}, pq.StringArray{}, "", true, pq.Array([]int{}), 0, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			m := map[string]any{}
			if err := rows.MapScan(m); err != nil {
				t.Fatal(err)
			}
			out = append(out, int(m["id"].(int64)))
		}
		return out
	}
	if got := ids(0); len(got) != 1 || got[0] != normal {
		t.Fatalf("list: %v", got)
	}
	if got := ids(step); len(got) != 1 || got[0] != step {
		t.Fatalf("by id: %v", got)
	}
}

// TestHiddenCampaignNotDeletedInBulk checks the bulk delete query skips step campaigns.
func TestHiddenCampaignNotDeletedInBulk(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	normal := mkCampaign(t, db, l)
	step := mkCampaign(t, db, l)
	if _, err := s.Create("a", l, step); err != nil {
		t.Fatal(err)
	}
	q := loadQuery(t, "../../queries/campaigns.sql", "delete-campaigns", nil)
	if _, err := db.Exec(q, pq.Array([]int{normal, step}), "", true, pq.Array([]int{}), nil); err != nil {
		t.Fatal(err)
	}
	var n []int
	if err := sqlx.Select(db, &n, `SELECT id FROM campaigns`); err != nil {
		t.Fatal(err)
	}
	if len(n) != 1 || n[0] != step {
		t.Fatalf("remaining: %v", n)
	}
}
