package brands

import (
	"errors"
	"testing"
)

func TestBootstrapIdempotentAndCRUD(t *testing.T) {
	db := testDB(t)
	if err := Bootstrap(db); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(db); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}

	s := NewStore(db)
	ri := Brand{Slug: "ri", Name: "RangeInsiders", ListTags: []string{"ri", "ri-cold"},
		Messengers: []string{"email-ri"}, FromDomains: []string{"rangeinsiders.com"}}
	if err := s.Upsert(ri); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get("ri")
	if err != nil || !ok || got.Name != "RangeInsiders" || len(got.ListTags) != 2 {
		t.Fatalf("get: %+v %v %v", got, ok, err)
	}

	if err := s.Upsert(Brand{Slug: "Bad Slug", Name: "x", ListTags: []string{}, Messengers: []string{}, FromDomains: []string{}}); err == nil {
		t.Fatal("bad slug accepted")
	}

	db.MustExec(`INSERT INTO templates (name, type, subject, body) VALUES ('t', 'campaign', '', '{{ template "content" . }}')`)
	var tid int
	if err := db.Get(&tid, `SELECT MAX(id) FROM templates`); err != nil {
		t.Fatal(err)
	}
	slug := "ri"
	if err := s.SetTemplateBrand(tid, &slug); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("ri"); !errors.Is(err, ErrBrandInUse) {
		t.Fatalf("want ErrBrandInUse for owned template, got %v", err)
	}
	if err := s.SetTemplateBrand(tid, nil); err != nil {
		t.Fatal(err)
	}
	owners, _ := s.TemplateOwners()
	if v, ok := owners[tid]; !ok || v != nil {
		t.Fatalf("want shared, got %v %v", v, ok)
	}

	// A list tagged with one of the brand's tags blocks the delete.
	db.MustExec(`INSERT INTO lists (uuid, name, type, optin, tags) VALUES (gen_random_uuid(), 'l', 'private', 'single', '{ri}')`)
	if err := s.Delete("ri"); !errors.Is(err, ErrBrandInUse) {
		t.Fatalf("want ErrBrandInUse for tagged list, got %v", err)
	}
	db.MustExec(`DELETE FROM lists`)

	// An owned media row blocks the delete.
	db.MustExec(`INSERT INTO media (uuid, filename, thumb, provider) VALUES (gen_random_uuid(), 'f.png', 't.png', 'filesystem')`)
	var mid int
	if err := db.Get(&mid, `SELECT MAX(id) FROM media`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMediaBrand(mid, &slug); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("ri"); !errors.Is(err, ErrBrandInUse) {
		t.Fatalf("want ErrBrandInUse for owned media, got %v", err)
	}
	if err := s.SetMediaBrand(mid, nil); err != nil {
		t.Fatal(err)
	}

	// An unowned brand deletes.
	if err := s.Delete("ri"); err != nil {
		t.Fatalf("delete unowned brand: %v", err)
	}
	if _, ok, _ := s.Get("ri"); ok {
		t.Fatal("brand still present after delete")
	}
}

func TestSeedInitialOnlyWhenEmpty(t *testing.T) {
	db := testDB(t)
	if err := Bootstrap(db); err != nil {
		t.Fatal(err)
	}
	seeded, err := SeedInitial(db)
	if err != nil || !seeded {
		t.Fatalf("first seed: %v %v", seeded, err)
	}
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM ext.brands`); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("want 3 brands, got %d", n)
	}

	// An admin edit must survive a restart: the second call does nothing.
	db.MustExec(`UPDATE ext.brands SET name = 'Edited' WHERE slug = 'ri'`)
	seeded, err = SeedInitial(db)
	if err != nil || seeded {
		t.Fatalf("second seed: %v %v", seeded, err)
	}
	var name string
	if err := db.Get(&name, `SELECT name FROM ext.brands WHERE slug = 'ri'`); err != nil {
		t.Fatal(err)
	}
	if name != "Edited" {
		t.Fatalf("seed overwrote an edit: %q", name)
	}
}
