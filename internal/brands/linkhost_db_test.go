package brands

import (
	"testing"

	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

// A database created before link_host existed gets the column on the next
// Bootstrap, existing rows read as "", and the value round-trips.
func TestLinkHostColumn(t *testing.T) {
	db := testDB(t)
	db.MustExec(`CREATE SCHEMA ext; CREATE TABLE ext.brands (
		slug TEXT PRIMARY KEY, name TEXT NOT NULL, list_tags TEXT[] NOT NULL DEFAULT '{}',
		messengers TEXT[] NOT NULL DEFAULT '{}', from_domains TEXT[] NOT NULL DEFAULT '{}',
		sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now());
		INSERT INTO ext.brands (slug, name) VALUES ('ta', 'Tactical Advisor')`)
	if err := Bootstrap(db); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	b, ok, err := s.Get("ta")
	if err != nil || !ok || b.LinkHost != "" {
		t.Fatalf("old row: %+v %v %v", b, ok, err)
	}
	b.LinkHost = "https://news.tacticaladvisor.net"
	if err := s.Upsert(b); err != nil {
		t.Fatal(err)
	}
	all, err := s.All()
	if err != nil || len(all) != 1 || all[0].LinkHost != "https://news.tacticaladvisor.net" {
		t.Fatalf("all: %+v %v", all, err)
	}
}

// The real send path: a running campaign fetched with next-campaigns carries no
// Lists, so the resolver must find TA through ext-campaign-list-ids.
func TestLinkResolverNextCampaignsPath(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(campaignFixtures); err != nil {
		t.Fatalf("fixtures: %v", err)
	}
	q := campaignTestQueries(t, db)
	db.MustExec(`INSERT INTO ext.brands (slug, name, list_tags, messengers, from_domains, link_host) VALUES
		('ri', 'RI', '{ri}', '{email}', '{b.c}', ''),
		('ta', 'TA', '{ta}', '{email-ta}', '{b.c}', 'https://news.tacticaladvisor.net')`)
	db.MustExec(`UPDATE campaigns SET status = 'running', messenger = 'email-ta' WHERE id = 2`)
	db.MustExec(`INSERT INTO subscribers (uuid, email, status) VALUES (gen_random_uuid(), 'x@b.c', 'enabled');
		INSERT INTO subscriber_lists (subscriber_id, list_id, status) SELECT id, 2, 'confirmed' FROM subscribers`)

	var camps []*models.Campaign
	if err := q.NextCampaigns.Unsafe().Select(&camps, pq.Int64Array{}, pq.Int64Array{}); err != nil {
		t.Fatalf("next-campaigns: %v", err)
	}
	if len(camps) != 1 || camps[0].ID != 2 || len(camps[0].Lists) != 0 {
		t.Fatalf("campaigns: %+v", camps)
	}
	svc := &Service{Store: NewStore(db)}
	resolve := svc.LinkResolver(func(id int) ([]int, error) {
		var ids []int
		err := q.ExtCampaignListIDs.Select(&ids, id)
		return ids, err
	})
	host, err := resolve(camps[0])
	if err != nil || host != "https://news.tacticaladvisor.net" {
		t.Fatalf("got %q, %v", host, err)
	}

	// A messenger reassigned away from TA makes the same campaign fail closed.
	db.MustExec(`UPDATE ext.brands SET messengers = '{email-x}' WHERE slug = 'ta'`)
	if _, err := resolve(camps[0]); err == nil {
		t.Fatal("messenger outside the brand must fail")
	}
}
