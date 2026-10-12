package brands

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/goyesql/v2"
	goyesqlx "github.com/knadh/goyesql/v2/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

// campaignTestQueries prepares every queries/*.sql into models.Queries as the
// app does at startup, mirroring the derived queries of cmd/init.go.
func campaignTestQueries(t *testing.T, db *sqlx.DB) *models.Queries {
	t.Helper()
	// Queries reference the ext schema, as at app startup.
	if err := Bootstrap(db); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	files, err := filepath.Glob("../../queries/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("find query files: %v", err)
	}
	qMap := goyesql.Queries{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		mp, err := goyesql.ParseBytes(b)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		maps.Copy(qMap, mp)
	}
	countQuery := qMap["get-campaign-analytics-counts"].Query
	qMap["get-campaign-view-counts"] = &goyesql.Query{
		Query: fmt.Sprintf(countQuery, "campaign_views"),
		Tags:  map[string]string{"name": "get-campaign-view-counts"},
	}
	qMap["get-campaign-click-counts"] = &goyesql.Query{
		Query: fmt.Sprintf(countQuery, "link_clicks"),
		Tags:  map[string]string{"name": "get-campaign-click-counts"},
	}
	qMap["get-campaign-link-counts"].Query = fmt.Sprintf(qMap["get-campaign-link-counts"].Query, "*")

	var q models.Queries
	if err := goyesqlx.ScanToStruct(&q, qMap, db); err != nil {
		t.Fatalf("prepare queries: %v", err)
	}
	return &q
}

const campaignFixtures = `
INSERT INTO lists (id, uuid, name, type, tags) VALUES
    (1, gen_random_uuid(), 'L1', 'public', '{ri}'),
    (2, gen_random_uuid(), 'L2', 'public', '{ta}');
INSERT INTO campaigns (id, uuid, name, subject, from_email, body, messenger, status) VALUES
    (1, gen_random_uuid(), 'A', 'S', 'a@b.c', 'b', 'email', 'draft'),
    (2, gen_random_uuid(), 'B', 'S', 'a@b.c', 'b', 'email', 'draft'),
    (3, gen_random_uuid(), 'M', 'S', 'a@b.c', 'b', 'email', 'draft'),
    (4, gen_random_uuid(), 'N', 'S', 'a@b.c', 'b', 'email', 'draft');
INSERT INTO campaign_lists (campaign_id, list_id, list_name) VALUES
    (1, 1, 'L1'), (2, 2, 'L2'), (3, 1, 'L1'), (3, 2, 'L2');
`

func TestCampaignPredicate(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(campaignFixtures); err != nil {
		t.Fatalf("fixtures: %v", err)
	}
	q := campaignTestQueries(t, db)

	run := func(brandLists interface{}) []string {
		stmt := strings.ReplaceAll(q.QueryCampaigns, "%order%", "c.id ASC")
		var rows models.Campaigns
		if err := db.Unsafe().Select(&rows, stmt, 0, pq.StringArray{}, pq.StringArray{}, "", true, pq.Array([]int{}), 0, 0, brandLists); err != nil {
			t.Fatalf("query: %v", err)
		}
		var names []string
		for _, r := range rows {
			names = append(names, r.Name)
			if r.Total != len(rows) {
				t.Fatalf("total %d, want %d", r.Total, len(rows))
			}
		}
		return names
	}

	// Super admin ($5 true) with brand lists {1}: only A. M (mixed) and N (no lists) never show.
	if got := run(pq.Array([]int{1})); !reflect.DeepEqual(got, []string{"A"}) {
		t.Fatalf("brand {1}: got %v", got)
	}
	// NULL returns all four.
	if got := run(nil); !reflect.DeepEqual(got, []string{"A", "B", "M", "N"}) {
		t.Fatalf("no brand: got %v", got)
	}

	// ext-campaign-in-brand.
	for _, c := range []struct {
		id   int
		want bool
	}{{1, true}, {3, false}, {4, false}, {2, false}} {
		var in bool
		if err := q.ExtCampaignInBrand.Get(&in, c.id, pq.Array([]int{1})); err != nil {
			t.Fatal(err)
		}
		if in != c.want {
			t.Fatalf("campaign %d in {1}: got %v want %v", c.id, in, c.want)
		}
	}

	// delete-campaigns with a brand: bulk delete by all only removes brand campaigns.
	if _, err := q.DeleteCampaigns.Exec(pq.Array([]int{}), "", true, pq.Array([]int{}), pq.Array([]int{1})); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := db.Get(&left, "SELECT COUNT(*) FROM campaigns"); err != nil || left != 3 {
		t.Fatalf("after branded delete: %d left (err %v), want 3", left, err)
	}
}
