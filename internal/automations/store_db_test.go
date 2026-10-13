package automations

import (
	"errors"
	"testing"
)

func TestCreateGetAll(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	c := mkCampaign(t, db, l)
	a, err := s.Create("TA welcome", l, c)
	if err != nil {
		t.Fatal(err)
	}
	if a.Enabled {
		t.Fatal("new automation must be disabled")
	}
	got, steps, err := s.Get(a.ID)
	if err != nil || got.Name != "TA welcome" || len(steps) != 1 || steps[0].CampaignID != c || steps[0].DelayMinutes != 0 {
		t.Fatalf("get: %+v %+v %v", got, steps, err)
	}
	all, err := s.All(nil)
	if err != nil || len(all) != 1 || all[0].ListName != "TA" {
		t.Fatalf("all nil: %v %v", all, err)
	}
	none, err := s.All([]int{})
	if err != nil || len(none) != 0 {
		t.Fatalf("all empty: %v %v", none, err)
	}
}

func TestOneEnabledPerList(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	a1, _ := s.Create("a1", l, mkCampaign(t, db, l))
	a2, _ := s.Create("a2", l, mkCampaign(t, db, l))
	if err := s.SetEnabled(a1.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled(a2.ID, true); !errors.Is(err, ErrEnabledOnList) {
		t.Fatalf("want ErrEnabledOnList, got %v", err)
	}
}

func TestDeleteRules(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	c := mkCampaign(t, db, l)
	a, _ := s.Create("a", l, c)
	_ = s.SetEnabled(a.ID, true)
	if _, err := s.Delete(a.ID); !errors.Is(err, ErrEnabled) {
		t.Fatalf("want ErrEnabled, got %v", err)
	}
	_ = s.SetEnabled(a.ID, false)
	ids, err := s.Delete(a.ID)
	if err != nil || len(ids) != 1 || ids[0] != c {
		t.Fatalf("delete: %v %v", ids, err)
	}
}

func TestStepOwnerAndCampaignRestrict(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	c := mkCampaign(t, db, l)
	a, _ := s.Create("a", l, c)
	info, ok, err := s.StepOwner(c)
	if err != nil || !ok || info.AutomationID != a.ID || info.ListID != l || info.Position != 1 || info.Enabled {
		t.Fatalf("owner: %+v %v %v", info, ok, err)
	}
	if _, ok, _ := s.StepOwner(c + 1000); ok {
		t.Fatal("unknown campaign must not be a step")
	}
	if _, err := db.Exec(`DELETE FROM campaigns WHERE id=$1`, c); err == nil {
		t.Fatal("deleting a step campaign must fail (ON DELETE RESTRICT)")
	}
}

func TestListChangeAfterEnrollment(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	l2 := mkList(t, db, "TA2", "single", "ta")
	a, _ := s.Create("a", l, mkCampaign(t, db, l))
	sub := mkSub(t, db, "x@example.com", l, "confirmed")
	if _, err := db.Exec(`INSERT INTO ext.automation_enrollments (automation_id, subscriber_id, status, next_at) VALUES ($1,$2,'active',now())`, a.ID, sub); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(a.ID, "a", l2, nil); !errors.Is(err, ErrHasEnrollments) {
		t.Fatalf("want ErrHasEnrollments, got %v", err)
	}
}

func TestSentByCampaign(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	c := mkCampaign(t, db, l)
	a, _ := s.Create("a", l, c)
	if n, err := s.SentByCampaign(c); err != nil || n != 0 {
		t.Fatalf("empty: %d %v", n, err)
	}
	for _, e := range []string{"a@example.com", "b@example.com"} {
		sub := mkSub(t, db, e, l, "confirmed")
		var enr int64
		if err := db.Get(&enr, `INSERT INTO ext.automation_enrollments (automation_id, subscriber_id, status) VALUES ($1,$2,'done') RETURNING id`, a.ID, sub); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO ext.automation_sends (enrollment_id, position, campaign_id) VALUES ($1, 1, $2)`, enr, c); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.SentByCampaign(c); err != nil || n != 2 {
		t.Fatalf("two sends: %d %v", n, err)
	}
	if n, err := s.SentByCampaign(c + 1000); err != nil || n != 0 {
		t.Fatalf("other campaign: %d %v", n, err)
	}
}

func TestListChangeMovesStepCampaigns(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	l2 := mkList(t, db, "TA2", "single", "ta")
	c := mkCampaign(t, db, l)
	a, _ := s.Create("a", l, c)
	if err := s.Update(a.ID, "a", l2, nil); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ListID   int    `db:"list_id"`
		ListName string `db:"list_name"`
	}
	if err := db.Select(&rows, `SELECT list_id, list_name FROM campaign_lists WHERE campaign_id=$1`, c); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ListID != l2 || rows[0].ListName != "TA2" {
		t.Fatalf("campaign_lists: %+v", rows)
	}
	if info, ok, _ := s.StepOwner(c); !ok || info.ListID != l2 {
		t.Fatalf("owner: %+v", info)
	}
}

// A list with an automation cannot be deleted; the step campaign must not become an orphan draft.
func TestListDeleteRefusedWithAutomation(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	c := mkCampaign(t, db, l)
	a, _ := s.Create("a", l, c)
	if _, err := db.Exec(`DELETE FROM lists WHERE id=$1`, l); err == nil {
		t.Fatal("deleting a list with an automation must fail")
	}
	if _, _, err := s.Get(a.ID); err != nil {
		t.Fatalf("the automation must stay: %v", err)
	}
	if _, ok, err := s.StepOwner(c); err != nil || !ok {
		t.Fatalf("the step campaign must stay a step: %v %v", ok, err)
	}
	if _, err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM lists WHERE id=$1`, l); err != nil {
		t.Fatalf("after the automation delete, the list delete must work: %v", err)
	}
}

// Bootstrap converts an older ON DELETE CASCADE list constraint to RESTRICT.
func TestBootstrapUpgradesListCascade(t *testing.T) {
	db := testDB(t)
	db.MustExec(`ALTER TABLE ext.automations DROP CONSTRAINT automations_list_id_fkey`)
	db.MustExec(`ALTER TABLE ext.automations ADD CONSTRAINT automations_list_id_fkey
		FOREIGN KEY (list_id) REFERENCES lists(id) ON DELETE CASCADE`)
	if err := Bootstrap(db); err != nil {
		t.Fatal(err)
	}
	var del string
	if err := db.Get(&del, `SELECT confdeltype::text FROM pg_constraint WHERE conname='automations_list_id_fkey'`); err != nil || del != "r" {
		t.Fatalf("want RESTRICT (r), got %q %v", del, err)
	}
}
