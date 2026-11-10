package brands

import "testing"

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
