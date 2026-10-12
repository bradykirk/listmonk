package brands

import (
	"encoding/json"
	"io"
	"log"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

const subFixtures = `
INSERT INTO lists (id, uuid, name, type, tags) VALUES
    (1, gen_random_uuid(), 'L1', 'public', '{ri}'),
    (2, gen_random_uuid(), 'L2', 'public', '{ta}');
INSERT INTO subscribers (id, uuid, email, first_name, status) VALUES
    (1, gen_random_uuid(), 's@example.com', 'S', 'enabled');
INSERT INTO subscriber_lists (subscriber_id, list_id, status) VALUES
    (1, 1, 'confirmed'), (1, 2, 'confirmed');
`

func subSetup(t *testing.T) (*sqlx.DB, *core.Core, *models.Queries) {
	t.Helper()
	db := testDB(t)
	if _, err := db.Exec(subFixtures); err != nil {
		t.Fatalf("fixtures: %v", err)
	}
	q := campaignTestQueries(t, db)
	c := core.New(&core.Opt{Queries: q, DB: db, Log: log.New(io.Discard, "", 0)}, &core.Hooks{
		SendOptinConfirmation: func(models.Subscriber, []int) (int, error) { return 0, nil },
	})
	return db, c, q
}

func subStatus(t *testing.T, db *sqlx.DB, sid, lid int) string {
	t.Helper()
	var s string
	if err := db.Get(&s, `SELECT status FROM subscriber_lists WHERE subscriber_id = $1 AND list_id = $2`, sid, lid); err != nil {
		t.Fatalf("status of list %d: %v", lid, err)
	}
	return s
}

func taManageUser() *auth.User {
	u := user([]string{"subscribers:get", "subscribers:manage"}, []int{1, 2}, []int{1, 2})
	sc := ScopeUser(u, []int{2}, allPerms)
	return &sc
}

func TestSubscriberUpdateKeepsOtherBrand(t *testing.T) {
	db, c, _ := subSetup(t)
	sub := models.Subscriber{Email: "s@example.com", FirstName: "S", Status: "enabled"}

	// TA view removes S from all TA lists; the RI subscription must remain.
	_, permitted := taManageUser().GetPermittedLists(auth.PermTypeManage)
	if _, _, err := c.UpdateSubscriberWithLists(1, sub, []int{}, nil, false, true, false, ScopedPermittedLists(permitted), false); err != nil {
		t.Fatal(err)
	}
	if got := subStatus(t, db, 1, 1); got != "confirmed" {
		t.Fatalf("RI subscription changed: %q", got)
	}
	var n int
	db.Get(&n, `SELECT COUNT(*) FROM subscriber_lists WHERE subscriber_id = 1 AND list_id = 2`)
	if n != 0 {
		t.Fatal("TA subscription not removed")
	}
}

func TestSubscriberUpdateNoManageRight(t *testing.T) {
	for _, patch := range []bool{false, true} {
		db, c, _ := subSetup(t)
		sub := models.Subscriber{Email: "s@example.com", FirstName: "S", Status: "enabled"}
		// Read on list 2 and no manage right: the permitted set is empty.
		u := ScopeUser(user([]string{"subscribers:get"}, []int{2}, nil), []int{2}, allPerms)
		up := &u
		_, permitted := up.GetPermittedLists(auth.PermTypeManage)
		if _, _, err := c.UpdateSubscriberWithLists(1, sub, []int{}, nil, false, true, false, ScopedPermittedLists(permitted), false); err != nil {
			t.Fatal(err)
		}
		for _, lid := range []int{1, 2} {
			if got := subStatus(t, db, 1, lid); got != "confirmed" {
				t.Fatalf("patch=%v list %d: %q", patch, lid, got)
			}
		}
	}
}

func TestManageSubscriberListsScoped(t *testing.T) {
	for _, action := range []string{"remove", "unsubscribe"} {
		db, c, _ := subSetup(t)
		ids := taManageUser().FilterListsByPerm(auth.PermTypeManage, []int{1, 2})
		if len(ids) != 1 || ids[0] != 2 {
			t.Fatalf("filtered: %v", ids)
		}
		var err error
		if action == "remove" {
			err = c.DeleteSubscriptions([]int{1}, ids)
		} else {
			err = c.UnsubscribeLists([]int{1}, ids, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := subStatus(t, db, 1, 1); got != "confirmed" {
			t.Fatalf("%s: RI subscription changed: %q", action, got)
		}
	}
}

func TestExtActivityAndExportScoped(t *testing.T) {
	db, _, q := subSetup(t)
	if _, err := db.Exec(`
INSERT INTO campaigns (id, uuid, name, subject, from_email, body, messenger, status) VALUES
    (1, gen_random_uuid(), 'A', 'SA', 'a@b.c', 'b', 'email', 'draft'),
    (2, gen_random_uuid(), 'B', 'SB', 'a@b.c', 'b', 'email', 'draft');
INSERT INTO campaign_lists (campaign_id, list_id, list_name) VALUES (1, 1, 'L1'), (2, 2, 'L2');
INSERT INTO links (id, uuid, url) VALUES (1, gen_random_uuid(), 'http://a'), (2, gen_random_uuid(), 'http://b');
INSERT INTO campaign_views (campaign_id, subscriber_id) VALUES (1, 1), (2, 1);
INSERT INTO link_clicks (campaign_id, link_id, subscriber_id) VALUES (1, 1, 1), (2, 2, 1);
`); err != nil {
		t.Fatal(err)
	}

	var act models.SubscriberActivity
	if err := q.ExtSubscriberActivity.Get(&act, 1, pq.Array([]int{2})); err != nil {
		t.Fatal(err)
	}
	var views, clicks []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	mustJSON(t, act.CampaignViews, &views)
	mustJSON(t, act.LinkClicks, &clicks)
	if len(views) != 1 || views[0].Name != "B" || len(clicks) != 1 || clicks[0].URL != "http://b" {
		t.Fatalf("activity: %s %s", act.CampaignViews, act.LinkClicks)
	}

	var exp models.SubscriberExportProfile
	if err := q.ExtExportSubscriberData.Get(&exp, 1, nil, pq.Array([]int{2})); err != nil {
		t.Fatal(err)
	}
	var subs []struct{ Name string }
	var ev []struct{ Campaign string }
	var ec []struct{ URL string }
	mustJSON(t, exp.Subscriptions, &subs)
	mustJSON(t, exp.CampaignViews, &ev)
	mustJSON(t, exp.LinkClicks, &ec)
	if len(subs) != 1 || subs[0].Name != "L2" || len(ev) != 1 || ev[0].Campaign != "SB" || len(ec) != 1 || ec[0].URL != "http://b" {
		t.Fatalf("export: %s %s %s", exp.Subscriptions, exp.CampaignViews, exp.LinkClicks)
	}
	if exp.Email != "s@example.com" || len(exp.Profile) == 0 {
		t.Fatalf("profile: %q %s", exp.Email, exp.Profile)
	}
}

func mustJSON(t *testing.T, b []byte, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("json %s: %v", b, err)
	}
}
