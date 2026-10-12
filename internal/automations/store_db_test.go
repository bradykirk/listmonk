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
	if err != nil || len(all) != 1 {
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
