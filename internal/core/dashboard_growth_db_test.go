package core

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/goyesql/v2"
	goyesqlx "github.com/knadh/goyesql/v2/sqlx"
	"github.com/knadh/listmonk/models"
	_ "github.com/lib/pq"
)

// TestDashboardGrowthSQL runs the get-dashboard-growth query against a real
// Postgres. It is skipped unless LISTMONK_TEST_DSN points at a server where the
// user may create databases, e.g.
//
//	LISTMONK_TEST_DSN=postgres://lmtest:lmtest@127.0.0.1:55432/lmtest?sslmode=disable
//
// The test installs schema.sql into a fresh, throwaway database and prepares
// every query in queries/*.sql into models.Queries, as the app does at startup,
// so a broken query fails here instead of stopping production from booting.
//
// It lives in package core, not cmd, because cmd's init() loads config.toml and
// exits when it is missing, which would break `go test ./...`.

type growthPoint struct {
	Date         string `json:"date"`
	Signups      int    `json:"signups"`
	Unsubscribes int    `json:"unsubscribes"`
	Audience     int    `json:"audience"`
}

type growthOut struct {
	Range        string        `json:"range"`
	Bucket       string        `json:"bucket"`
	TZ           string        `json:"tz"`
	AudienceNow  int           `json:"audience_now"`
	Signups      int           `json:"signups"`
	Unsubscribes int           `json:"unsubscribes"`
	Net          int           `json:"net"`
	Series       []growthPoint `json:"series"`
}

// growthNow is the fixed clock for every case: 2026-11-10 12:00 in Chicago
// (CST, UTC-6). Daylight saving time ended in Chicago on 2026-11-01, inside the
// 30 day range, so day boundaries on both sides of the change are exercised.
var growthNow = time.Date(2026, 11, 10, 18, 0, 0, 0, time.UTC)

// Fixtures. All timestamps are UTC. The comment on each person gives the
// Chicago date that counts and the expected effect.
const growthFixtures = `
INSERT INTO lists (id, uuid, name, type) VALUES
    (1, gen_random_uuid(), 'L1', 'public'),
    (2, gen_random_uuid(), 'L2', 'public');

INSERT INTO campaigns (id, uuid, name, subject, from_email, body, messenger, status)
    VALUES (1, gen_random_uuid(), 'C1', 'S', 'a@b.c', 'b', 'email', 'finished');

INSERT INTO subscribers (id, uuid, email, first_name, status, created_at, updated_at) VALUES
    -- P1  joined 2026-09-01 (before range), active.
    (1,  gen_random_uuid(), 'p1@x.test',  'p1',  'enabled',     '2026-09-01T12:00Z', '2026-09-01T12:00Z'),
    -- P2  joined 2026-10-20, unconfirmed double opt-in still counts, active.
    (2,  gen_random_uuid(), 'p2@x.test',  'p2',  'enabled',     '2026-10-20T15:00Z', '2026-10-20T15:00Z'),
    -- P3  joined 2026-10-15, unsubscribed from its only list 2026-10-25.
    (3,  gen_random_uuid(), 'p3@x.test',  'p3',  'enabled',     '2026-10-15T14:00Z', '2026-10-15T14:00Z'),
    -- P4  joined 2026-10-13, unsubscribed from one of two lists: still active.
    (4,  gen_random_uuid(), 'p4@x.test',  'p4',  'enabled',     '2026-10-13T12:00Z', '2026-10-13T12:00Z'),
    -- P5  joined 2026-08-01, blocklisted via a campaign unsubscribe link on
    --     2026-11-03. That path never bumps subscribers.updated_at, so a rule
    --     based on it would wrongly treat P5 as blocklisted at creation.
    (5,  gen_random_uuid(), 'p5@x.test',  'p5',  'blocklisted', '2026-08-01T12:00Z', '2026-08-01T12:00:10Z'),
    -- P6  blocklisted at creation: excluded entirely.
    (6,  gen_random_uuid(), 'p6@x.test',  'p6',  'blocklisted', '2026-10-22T12:00Z', '2026-10-22T12:00Z'),
    -- P7  orphan, no lists: excluded entirely.
    (7,  gen_random_uuid(), 'p7@x.test',  'p7',  'enabled',     '2026-10-23T12:00Z', '2026-10-23T12:00Z'),
    -- P8  joined 2026-10-14, disabled by an admin edit 2026-11-05.
    (8,  gen_random_uuid(), 'p8@x.test',  'p8',  'disabled',    '2026-10-14T12:00Z', '2026-11-05T12:00Z'),
    -- P9  2026-10-28T04:30Z is 2026-10-27 23:30 in Chicago: counts on the 27th.
    (9,  gen_random_uuid(), 'p9@x.test',  'p9',  'enabled',     '2026-10-28T04:30Z', '2026-10-28T04:30Z'),
    -- P10 joined 2026-10-16, blocklisted by a bounce 2026-11-08. The bounce
    --     path touches neither updated_at column; only bounces.created_at dates it.
    (10, gen_random_uuid(), 'p10@x.test', 'p10', 'blocklisted', '2026-10-16T12:00Z', '2026-10-16T12:00Z'),
    -- P11 2026-11-01T04:30Z is 2026-10-31 23:30 CDT: counts on the 31st.
    (11, gen_random_uuid(), 'p11@x.test', 'p11', 'enabled',     '2026-11-01T04:30Z', '2026-11-01T04:30Z'),
    -- P12 2026-11-02T05:30Z is 2026-11-01 23:30 CST: counts on 11-01.
    (12, gen_random_uuid(), 'p12@x.test', 'p12', 'enabled',     '2026-11-02T05:30Z', '2026-11-02T05:30Z'),
    -- P13 created in the future (clock skew): clamped into the last bucket, active.
    (13, gen_random_uuid(), 'p13@x.test', 'p13', 'enabled',     '2026-11-11T12:00Z', '2026-11-11T12:00Z'),
    -- P14 joined 2026-09-05 and left 2026-09-20, both before the range.
    (14, gen_random_uuid(), 'p14@x.test', 'p14', 'enabled',     '2026-09-05T12:00Z', '2026-09-05T12:00Z');

INSERT INTO subscriber_lists (subscriber_id, list_id, status, created_at, updated_at) VALUES
    (1,  1, 'confirmed',    '2026-09-01T12:00Z', '2026-09-01T12:00Z'),
    (2,  1, 'unconfirmed',  '2026-10-20T15:00Z', '2026-10-20T15:00Z'),
    (3,  1, 'unsubscribed', '2026-10-15T14:00Z', '2026-10-25T16:00Z'),
    (4,  1, 'unsubscribed', '2026-10-13T12:00Z', '2026-10-30T12:00Z'),
    (4,  2, 'confirmed',    '2026-10-13T12:00Z', '2026-10-13T12:00Z'),
    -- P5's only list was already unsubscribed on 2026-10-01, before the range.
    -- The later blocklist-by-link skips rows that are already unsubscribed, so
    -- only campaign_unsubs dates the exit (2026-11-03). Without that evidence
    -- the exit would land before the range and the expected values would break.
    (5,  1, 'unsubscribed', '2026-08-01T12:00Z', '2026-10-01T12:00Z'),
    (6,  1, 'unsubscribed', '2026-10-22T12:00Z', '2026-10-22T12:00Z'),
    (8,  1, 'confirmed',    '2026-10-14T12:00Z', '2026-10-14T12:00Z'),
    (9,  1, 'confirmed',    '2026-10-28T04:30Z', '2026-10-28T04:30Z'),
    (10, 1, 'confirmed',    '2026-10-16T12:00Z', '2026-10-16T12:00Z'),
    (11, 1, 'confirmed',    '2026-11-01T04:30Z', '2026-11-01T04:30Z'),
    (12, 1, 'confirmed',    '2026-11-02T05:30Z', '2026-11-02T05:30Z'),
    (13, 1, 'confirmed',    '2026-11-11T12:00Z', '2026-11-11T12:00Z'),
    (14, 1, 'unsubscribed', '2026-09-05T12:00Z', '2026-09-20T12:00Z');

INSERT INTO campaign_unsubs (campaign_id, subscriber_id, created_at)
    VALUES (1, 5, '2026-11-03T20:00Z');

INSERT INTO bounces (subscriber_id, campaign_id, type, created_at)
    VALUES (10, 1, 'hard', '2026-11-08T12:00Z');
`

func TestDashboardGrowthSQL(t *testing.T) {
	dsn := os.Getenv("LISTMONK_TEST_DSN")
	if dsn == "" {
		t.Skip("LISTMONK_TEST_DSN not set")
	}

	q := growthTestDB(t, dsn)

	run := func(t *testing.T, tz string, r GrowthRange) growthOut {
		t.Helper()

		var raw []byte
		if err := q.GetDashboardGrowth.Get(&raw, tz, r.Buckets, r.Unit, growthNow, r.Label); err != nil {
			t.Fatalf("get-dashboard-growth(%s, %s): %v", tz, r.Label, err)
		}

		var out growthOut
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal: %v\n%s", err, raw)
		}
		checkGrowthInvariants(t, out, r)

		return out
	}

	t.Run("30d Chicago", func(t *testing.T) {
		out := run(t, "America/Chicago", growthRanges["30d"])

		if out.Range != "30d" || out.Bucket != "day" || out.TZ != "America/Chicago" {
			t.Errorf("echoed params = %q %q %q", out.Range, out.Bucket, out.TZ)
		}
		wantTotals(t, out, 7, 9, 4, 5)

		if first, last := out.Series[0].Date, out.Series[len(out.Series)-1].Date; first != "2026-10-12" || last != "2026-11-10" {
			t.Errorf("series spans %s..%s, want 2026-10-12..2026-11-10", first, last)
		}

		// Expected Chicago-day signups, unsubscribes and end-of-day audience.
		// Days not listed have 0 signups and 0 unsubscribes.
		signups := map[string]int{
			"2026-10-13": 1, "2026-10-14": 1, "2026-10-15": 1, "2026-10-16": 1, "2026-10-20": 1,
			"2026-10-27": 1, "2026-10-31": 1, "2026-11-01": 1, "2026-11-10": 1,
		}
		unsubs := map[string]int{"2026-10-25": 1, "2026-11-03": 1, "2026-11-05": 1, "2026-11-08": 1}
		audience := map[string]int{
			"2026-10-12": 2, "2026-10-13": 3, "2026-10-14": 4, "2026-10-15": 5, "2026-10-16": 6,
			"2026-10-19": 6, "2026-10-20": 7, "2026-10-24": 7, "2026-10-25": 6, "2026-10-26": 6,
			"2026-10-27": 7, "2026-10-28": 7, "2026-10-30": 7, "2026-10-31": 8, "2026-11-01": 9,
			"2026-11-02": 9, "2026-11-03": 8, "2026-11-04": 8, "2026-11-05": 7, "2026-11-07": 7,
			"2026-11-08": 6, "2026-11-09": 6, "2026-11-10": 7,
		}

		for _, p := range out.Series {
			if p.Signups != signups[p.Date] {
				t.Errorf("%s signups = %d, want %d", p.Date, p.Signups, signups[p.Date])
			}
			if p.Unsubscribes != unsubs[p.Date] {
				t.Errorf("%s unsubscribes = %d, want %d", p.Date, p.Unsubscribes, unsubs[p.Date])
			}
			if want, ok := audience[p.Date]; ok && p.Audience != want {
				t.Errorf("%s audience = %d, want %d", p.Date, p.Audience, want)
			}
		}
	})

	t.Run("30d UTC buckets differ from Chicago", func(t *testing.T) {
		out := run(t, "UTC", growthRanges["30d"])
		wantTotals(t, out, 7, 9, 4, 5)

		byDate := map[string]growthPoint{}
		for _, p := range out.Series {
			byDate[p.Date] = p
		}
		// P9 is 2026-10-28 in UTC but 2026-10-27 in Chicago.
		if byDate["2026-10-27"].Signups != 0 || byDate["2026-10-28"].Signups != 1 {
			t.Errorf("UTC signups 10-27=%d 10-28=%d, want 0 and 1",
				byDate["2026-10-27"].Signups, byDate["2026-10-28"].Signups)
		}
	})

	t.Run("7d Chicago", func(t *testing.T) {
		out := run(t, "America/Chicago", growthRanges["7d"])
		wantTotals(t, out, 7, 1, 2, -1)

		if out.Series[0].Date != "2026-11-04" {
			t.Errorf("first bucket = %s, want 2026-11-04", out.Series[0].Date)
		}
		// Audience at the end of 11-03 was 8; 11-04 adds nothing.
		if out.Series[0].Audience != 8 {
			t.Errorf("11-04 audience = %d, want 8", out.Series[0].Audience)
		}
	})

	t.Run("12m weekly Chicago", func(t *testing.T) {
		out := run(t, "America/Chicago", growthRanges["12m"])

		if out.Bucket != "week" {
			t.Errorf("bucket = %q, want week", out.Bucket)
		}
		// Every included person joined inside the 52 weeks; P3, P5, P8, P10
		// and P14 left inside them.
		wantTotals(t, out, 7, 12, 5, 7)

		if first, last := out.Series[0].Date, out.Series[len(out.Series)-1].Date; first != "2025-11-17" || last != "2026-11-09" {
			t.Errorf("series spans %s..%s, want 2025-11-17..2026-11-09 (Mondays)", first, last)
		}

		for _, p := range out.Series {
			// Week of Monday 2026-10-26: P9 (10-27), P11 (10-31), P12 (11-01).
			if p.Date == "2026-10-26" && p.Signups != 3 {
				t.Errorf("week of 2026-10-26 signups = %d, want 3", p.Signups)
			}
		}
	})
}

// wantTotals checks the headline numbers.
func wantTotals(t *testing.T, out growthOut, audienceNow, signups, unsubs, net int) {
	t.Helper()

	if out.AudienceNow != audienceNow || out.Signups != signups || out.Unsubscribes != unsubs || out.Net != net {
		t.Errorf("totals = now %d, +%d, -%d, net %d; want now %d, +%d, -%d, net %d",
			out.AudienceNow, out.Signups, out.Unsubscribes, out.Net,
			audienceNow, signups, unsubs, net)
	}
}

// checkGrowthInvariants checks the rules that must hold for any data set.
func checkGrowthInvariants(t *testing.T, out growthOut, r GrowthRange) {
	t.Helper()

	if len(out.Series) != r.Buckets {
		t.Fatalf("series has %d points, want %d", len(out.Series), r.Buckets)
	}

	var sumSignups, sumUnsubs int
	for i, p := range out.Series {
		sumSignups += p.Signups
		sumUnsubs += p.Unsubscribes

		if i > 0 {
			prev := out.Series[i-1]
			if p.Audience-prev.Audience != p.Signups-p.Unsubscribes {
				t.Errorf("%s: audience moved %d but signups-unsubscribes = %d",
					p.Date, p.Audience-prev.Audience, p.Signups-p.Unsubscribes)
			}
		}
	}

	if sumSignups != out.Signups || sumUnsubs != out.Unsubscribes {
		t.Errorf("series sums +%d -%d do not match totals +%d -%d", sumSignups, sumUnsubs, out.Signups, out.Unsubscribes)
	}
	if out.Net != out.Signups-out.Unsubscribes {
		t.Errorf("net %d != signups %d - unsubscribes %d", out.Net, out.Signups, out.Unsubscribes)
	}
	if last := out.Series[len(out.Series)-1].Audience; last != out.AudienceNow {
		t.Errorf("last series point %d != audience_now %d", last, out.AudienceNow)
	}
}

func growthTestDB(t *testing.T, dsn string) *models.Queries {
	t.Helper()

	_, q := newTestDB(t, dsn, "lm_growth_test", growthFixtures)
	return q
}

// newTestDB creates a throwaway database, installs schema.sql and fixtures
// (which may be empty), and prepares every query in queries/*.sql into
// models.Queries as the app does at startup.
func newTestDB(t *testing.T, dsn, prefix, fixtures string) (*sqlx.DB, *models.Queries) {
	t.Helper()

	admin, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close() })

	name := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
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
	if fixtures != "" {
		if _, err := db.Exec(fixtures); err != nil {
			t.Fatalf("fixtures: %v", err)
		}
	}

	files, err := filepath.Glob("../../queries/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("find query files: %v (%d found)", err, len(files))
	}

	qMap := goyesql.Queries{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		mp, err := goyesql.ParseBytes(b)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		maps.Copy(qMap, mp)
	}

	// Mirror the three derived queries that cmd/init.go prepareQueries adds
	// before scanning, with privacy.individual_tracking off. If that function
	// changes, ScanToStruct below fails loudly on the missing query.
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
		t.Fatalf("prepare queries (the app would fail to start): %v", err)
	}

	return db, &q
}
