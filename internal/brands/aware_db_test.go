package brands

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// Brands ri ({ri}) and ta ({ta}); list 1 is ri, list 2 is ta. Campaign A
// (id 1) goes to list 1 and B (id 2) to list 2. Subscriber 1 (a@x.com) is on
// list 1 and subscriber 2 (b@y.com) on list 2.
const awareFixtures = `
INSERT INTO ext.brands (slug, name, list_tags) VALUES ('ri', 'RI', '{ri}'), ('ta', 'TA', '{ta}');
INSERT INTO lists (id, uuid, name, type, optin, tags) VALUES
    (1, gen_random_uuid(), 'L1', 'public', 'single', '{ri}'),
    (2, gen_random_uuid(), 'L2', 'private', 'double', '{ta}');
INSERT INTO subscribers (id, uuid, email) VALUES
    (1, gen_random_uuid(), 'a@x.com'),
    (2, gen_random_uuid(), 'b@y.com');
INSERT INTO subscriber_lists (subscriber_id, list_id, status) VALUES (1, 1, 'confirmed'), (2, 2, 'confirmed');
INSERT INTO templates (id, name, type, subject, body) VALUES
    (101, 'T1', 'campaign', '', 'x'), (102, 'T2', 'campaign', '', 'x'),
    (103, 'T3', 'campaign', '', 'x'), (104, 'T4', 'campaign', '', 'x');
INSERT INTO ext.template_brands (template_id, brand_slug) VALUES (101, 'ri'), (102, NULL), (103, 'ta');
INSERT INTO media (id, uuid, provider, filename, thumb) VALUES
    (201, gen_random_uuid(), 'filesystem', 'm1.png', ''), (202, gen_random_uuid(), 'filesystem', 'm2.png', ''),
    (203, gen_random_uuid(), 'filesystem', 'm3.png', ''), (204, gen_random_uuid(), 'filesystem', 'm4.png', '');
INSERT INTO ext.media_brands (media_id, brand_slug) VALUES (201, 'ri'), (202, NULL), (203, 'ta');
INSERT INTO campaigns (id, uuid, name, subject, from_email, body, messenger, status, sent, started_at) VALUES
    (1, gen_random_uuid(), 'A', 'S', 'a@b.c', 'b', 'email', 'finished', 10, NOW() - INTERVAL '2 hours'),
    (2, gen_random_uuid(), 'B', 'S', 'a@b.c', 'b', 'email', 'finished', 20, NOW() - INTERVAL '2 hours');
INSERT INTO campaign_lists (campaign_id, list_id, list_name) VALUES (1, 1, 'L1'), (2, 2, 'L2');
INSERT INTO campaign_views (campaign_id, subscriber_id) VALUES (1, 1), (2, 2), (2, 2);
INSERT INTO links (id, uuid, url) VALUES (1, gen_random_uuid(), 'https://a'), (2, gen_random_uuid(), 'https://b');
INSERT INTO link_clicks (campaign_id, link_id, subscriber_id) VALUES (1, 1, 1), (2, 2, 2);
INSERT INTO bounces (id, subscriber_id, campaign_id, type) VALUES (1, 1, 1, 'hard'), (2, 2, 2, 'hard'), (3, 2, NULL, 'soft');
`

func awareDB(t *testing.T) (*sqlx.DB, *models.Queries) {
	t.Helper()
	db := testDB(t)
	q := campaignTestQueries(t, db) // runs Bootstrap
	if _, err := db.Exec(awareFixtures); err != nil {
		t.Fatalf("fixtures: %v", err)
	}
	return db, q
}

func getJSON(t *testing.T, stmt *sqlx.Stmt, args ...any) any {
	t.Helper()
	var raw []byte
	if err := stmt.Get(&raw, args...); err != nil {
		t.Fatalf("query: %v", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func sumField(rows any, field string) float64 {
	var n float64
	for _, r := range rows.([]any) {
		n += r.(map[string]any)[field].(float64)
	}
	return n
}

var ri = pq.Array([]int{1})

func TestBrandAwareTemplatesAndMedia(t *testing.T) {
	db, q := awareDB(t)
	s := NewStore(db)

	owners, err := s.TemplateOwners()
	if err != nil {
		t.Fatal(err)
	}
	var vis []int
	for _, id := range []int{101, 102, 103, 104} {
		if Visible(owners, "ri", id) {
			vis = append(vis, id)
		}
	}
	if !reflect.DeepEqual(vis, []int{101, 102}) {
		t.Fatalf("templates visible in ri: %v", vis)
	}
	for id, want := range map[int]bool{101: true, 102: true, 103: false, 104: false} {
		got, err := s.TemplateVisible(id, "ri")
		if err != nil || got != want {
			t.Errorf("TemplateVisible(%d, ri) = %v, %v; want %v", id, got, err, want)
		}
		got, err = s.MediaVisible(id+100, "ri")
		if err != nil || got != want {
			t.Errorf("MediaVisible(%d, ri) = %v, %v; want %v", id+100, got, err, want)
		}
	}

	media := func(brand any) []int {
		var rows []struct {
			ID    int `db:"id"`
			Total int `db:"total"`
		}
		if err := q.QueryMedia.Unsafe().Select(&rows, "%%", "filesystem", 0, 50, brand); err != nil {
			t.Fatalf("query-media: %v", err)
		}
		var ids []int
		for _, r := range rows {
			ids = append(ids, r.ID)
			if r.Total != len(rows) {
				t.Fatalf("total %d, want %d", r.Total, len(rows))
			}
		}
		sort.Ints(ids)
		return ids
	}
	if got := media("ri"); !reflect.DeepEqual(got, []int{201, 202}) {
		t.Fatalf("media in ri: %v", got)
	}
	if got := media(nil); !reflect.DeepEqual(got, []int{201, 202, 203, 204}) {
		t.Fatalf("media, no brand: %v", got)
	}
}

func TestBrandAwareBounces(t *testing.T) {
	db, q := awareDB(t)
	stmt := strings.ReplaceAll(q.QueryBounces, "%order%", "bounces.id ASC")

	run := func(brand any) []int {
		var rows []models.Bounce
		if err := db.Select(&rows, stmt, 0, 0, 0, "", 0, 0, brand); err != nil {
			t.Fatalf("query-bounces: %v", err)
		}
		var ids []int
		for _, r := range rows {
			ids = append(ids, r.ID)
			if r.Total != len(rows) {
				t.Fatalf("total %d, want %d", r.Total, len(rows))
			}
		}
		return ids
	}
	if got := run(ri); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("bounces in {1}: %v", got)
	}
	if got := run(pq.Array([]int{})); len(got) != 0 {
		t.Fatalf("bounces in {}: %v", got)
	}
	if got := run(nil); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("bounces, no brand: %v", got)
	}
	// A bounce with no campaign shows when its subscriber is on a brand list.
	db.MustExec(`INSERT INTO bounces (id, subscriber_id, campaign_id, type) VALUES (4, 1, NULL, 'soft')`)
	if got := run(ri); !reflect.DeepEqual(got, []int{1, 4}) {
		t.Fatalf("bounces in {1} with a campaign-less bounce: %v", got)
	}

	// Delete all in a brand view removes only the brand's bounces.
	if _, err := q.DeleteBounces.Exec(pq.Array([]int{}), true, ri); err != nil {
		t.Fatal(err)
	}
	if got := run(nil); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("after branded delete: %v", got)
	}
	// Delete by ID in a brand view ignores IDs outside the brand.
	if _, err := q.DeleteBounces.Exec(pq.Array([]int{2, 3}), false, ri); err != nil {
		t.Fatal(err)
	}
	if got := run(nil); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("after branded delete by ID: %v", got)
	}
	// No brand: upstream behavior.
	if _, err := q.DeleteBounces.Exec(pq.Array([]int{2}), false, nil); err != nil {
		t.Fatal(err)
	}
	if got := run(nil); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("after unbranded delete: %v", got)
	}
}

func TestBrandAwareDashboard(t *testing.T) {
	_, q := awareDB(t)

	c := getJSON(t, q.ExtDashboardCounts, ri).(map[string]any)
	subs := c["subscribers"].(map[string]any)
	if subs["total"].(float64) != 1 {
		t.Fatalf("subscribers.total: %v", subs)
	}
	lists := c["lists"].(map[string]any)
	if lists["total"].(float64) != 1 || lists["public"].(float64) != 1 || lists["private"].(float64) != 0 ||
		lists["optin_single"].(float64) != 1 || lists["optin_double"].(float64) != 0 {
		t.Fatalf("lists: %v", lists)
	}
	camps := c["campaigns"].(map[string]any)
	if camps["total"].(float64) != 1 || !reflect.DeepEqual(camps["by_status"], map[string]any{"finished": float64(1)}) {
		t.Fatalf("campaigns: %v", camps)
	}
	if c["messages"].(float64) != 10 {
		t.Fatalf("messages: %v", c["messages"])
	}
	for _, k := range []string{"blocklisted", "orphans"} {
		if _, ok := subs[k]; !ok {
			t.Fatalf("subscribers.%s missing: %v", k, subs)
		}
	}

	// Empty brand matches nothing.
	c = getJSON(t, q.ExtDashboardCounts, pq.Array([]int{})).(map[string]any)
	if c["subscribers"].(map[string]any)["total"] != nil && c["subscribers"].(map[string]any)["total"].(float64) != 0 {
		t.Fatalf("empty brand subscribers: %v", c["subscribers"])
	}

	ch := getJSON(t, q.ExtDashboardCharts, ri).(map[string]any)
	if n := sumField(ch["campaign_views"], "count"); n != 1 {
		t.Fatalf("chart views in {1}: %v", n)
	}
	if n := sumField(ch["link_clicks"], "count"); n != 1 {
		t.Fatalf("chart clicks in {1}: %v", n)
	}
}

func TestBrandAwareAnalytics(t *testing.T) {
	_, q := awareDB(t)

	// growth: joined only on list 1.
	if n := sumField(getJSON(t, q.GetAnalyticsGrowth, 0, ri), "joined"); n != 1 {
		t.Errorf("growth {1}: joined %v", n)
	}
	if n := sumField(getJSON(t, q.GetAnalyticsGrowth, 0, nil), "joined"); n != 2 {
		t.Errorf("growth, no brand: joined %v", n)
	}

	// campaigns: only A.
	var names []string
	for _, r := range getJSON(t, q.GetAnalyticsCampaigns, ri).([]any) {
		names = append(names, r.(map[string]any)["name"].(string))
	}
	if !reflect.DeepEqual(names, []string{"A"}) {
		t.Errorf("campaigns {1}: %v", names)
	}
	if n := len(getJSON(t, q.GetAnalyticsCampaigns, nil).([]any)); n != 2 {
		t.Errorf("campaigns, no brand: %d", n)
	}

	// cohorts: one member, active through campaign A.
	if n := sumField(getJSON(t, q.GetAnalyticsCohorts, 0, ri), "subscribers"); n != 1 {
		t.Errorf("cohorts {1}: %v", n)
	}

	// timeline and links: B is excluded even when asked for by ID.
	if n := sumField(getJSON(t, q.GetAnalyticsCampaignTimeline, 2, ri), "opens"); n != 0 {
		t.Errorf("timeline B in {1}: opens %v", n)
	}
	if n := sumField(getJSON(t, q.GetAnalyticsCampaignTimeline, 1, ri), "opens"); n != 1 {
		t.Errorf("timeline A in {1}: opens %v", n)
	}
	if n := len(getJSON(t, q.GetAnalyticsCampaignLinks, 2, ri).([]any)); n != 0 {
		t.Errorf("links B in {1}: %d rows", n)
	}
	if n := len(getJSON(t, q.GetAnalyticsCampaignLinks, 1, ri).([]any)); n != 1 {
		t.Errorf("links A in {1}: %d rows", n)
	}

	// activity: only campaign A events.
	for _, r := range getJSON(t, q.GetAnalyticsActivity, 0, ri).([]any) {
		if c := r.(map[string]any)["campaign"]; c != "A" {
			t.Errorf("activity {1}: event from %v", c)
		}
	}
	if n := len(getJSON(t, q.GetAnalyticsActivity, 0, nil).([]any)); n != 5 {
		t.Errorf("activity, no brand: %d events", n)
	}

	// summary.
	sm := getJSON(t, q.GetAnalyticsSummary, 0, ri).(map[string]any)
	want := map[string]float64{"subscribers": 1, "campaigns_90d": 1, "sent_90d": 10, "opens_90d": 1, "clicks_90d": 1, "bounces_90d": 1}
	for k, v := range want {
		if sm[k].(float64) != v {
			t.Errorf("summary {1} %s = %v, want %v", k, sm[k], v)
		}
	}
	if sm := getJSON(t, q.GetAnalyticsSummary, 0, nil).(map[string]any); sm["sent_90d"].(float64) != 30 {
		t.Errorf("summary, no brand: sent_90d %v", sm["sent_90d"])
	}

	// domains: only x.com.
	for _, r := range getJSON(t, q.GetAnalyticsDomains, 0, ri).([]any) {
		if d := r.(map[string]any)["domain"]; d != "x.com" {
			t.Errorf("domains {1}: %v", d)
		}
	}

	// send times: one campaign, 10 sent.
	st := getJSON(t, q.GetAnalyticsSendTimes, ri)
	if sumField(st, "campaigns") != 1 || sumField(st, "sent") != 10 {
		t.Errorf("send times {1}: %v", st)
	}

	// dashboard growth.
	g := getJSON(t, q.GetDashboardGrowth, "UTC", 7, "day", time.Now(), "7d", ri).(map[string]any)
	if g["audience_now"].(float64) != 1 {
		t.Errorf("dashboard growth {1}: audience_now %v", g["audience_now"])
	}
	g = getJSON(t, q.GetDashboardGrowth, "UTC", 7, "day", time.Now(), "7d", nil).(map[string]any)
	if g["audience_now"].(float64) != 2 {
		t.Errorf("dashboard growth, no brand: audience_now %v", g["audience_now"])
	}
}

type nopStore struct{}

func (nopStore) Put(string, string, io.ReadSeeker) (string, error) { return "", nil }
func (nopStore) Delete(string) error                               { return nil }
func (nopStore) GetURL(s string) string                            { return "/" + s }
func (nopStore) GetBlob(string) ([]byte, error)                    { return nil, nil }

var _ media.Store = nopStore{}

func testCore(t *testing.T, db *sqlx.DB, q *models.Queries) *core.Core {
	t.Helper()
	b, err := os.ReadFile("../../i18n/en.json")
	if err != nil {
		t.Fatal(err)
	}
	in, err := i18n.New(b)
	if err != nil {
		t.Fatal(err)
	}
	return core.New(&core.Opt{DB: db, Queries: q, I18n: in, Log: log.New(io.Discard, "", 0)}, &core.Hooks{})
}

// Review Focus 9: a failed ownership insert leaves no template or media row.
func TestCreateOwnedIsAtomic(t *testing.T) {
	db, q := awareDB(t)
	co := testCore(t, db, q)

	count := func(table string) int {
		var n int
		if err := db.Get(&n, "SELECT COUNT(*) FROM "+table); err != nil {
			t.Fatal(err)
		}
		return n
	}
	nope := "nope"

	tBefore := count("templates")
	if _, err := co.CreateTemplateOwned("x", "campaign", "", []byte("x"), null.String{}, &nope, true); err == nil {
		t.Fatal("template with unknown owner: no error")
	}
	if n := count("templates"); n != tBefore {
		t.Fatalf("templates %d after failed create, want %d", n, tBefore)
	}

	mBefore := count("media")
	if _, err := co.InsertMediaOwned("f.png", "", "image/png", models.JSON{}, "filesystem", nopStore{}, &nope, true); err == nil {
		t.Fatal("media with unknown owner: no error")
	}
	if n := count("media"); n != mBefore {
		t.Fatalf("media %d after failed insert, want %d", n, mBefore)
	}

	ta := "ta"
	tpl, err := co.CreateTemplateOwned("y", "campaign", "", []byte("y"), null.String{}, &ta, true)
	if err != nil {
		t.Fatal(err)
	}
	m, err := co.InsertMediaOwned("g.png", "", "image/png", models.JSON{}, "filesystem", nopStore{}, &ta, true)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	if ok, err := s.TemplateVisible(tpl.ID, "ta"); err != nil || !ok {
		t.Fatalf("new template not owned by ta: %v %v", ok, err)
	}
	if ok, err := s.MediaVisible(m.ID, "ta"); err != nil || !ok {
		t.Fatalf("new media not owned by ta: %v %v", ok, err)
	}

	// Shared (nil owner, assign) and unassigned (no assign).
	shared, err := co.CreateTemplateOwned("z", "campaign", "", []byte("z"), null.String{}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	un, err := co.CreateTemplateOwned("w", "campaign", "", []byte("w"), null.String{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	owners, err := s.TemplateOwners()
	if err != nil {
		t.Fatal(err)
	}
	if o, ok := owners[shared.ID]; !ok || o != nil {
		t.Fatalf("shared template owner: %v %v", o, ok)
	}
	if _, ok := owners[un.ID]; ok {
		t.Fatal("unassigned template has an owner row")
	}
}
