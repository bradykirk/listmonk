package brands

import (
	"errors"
	"io"
	"log"
	"reflect"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/models"
)

type fakeStore struct {
	manager.Store
	next   []*models.Campaign
	status map[int]string
}

func (f *fakeStore) NextCampaigns(cur, sent []int64) ([]*models.Campaign, error) { return f.next, nil }
func (f *fakeStore) UpdateCampaignStatus(id int, s string) error {
	f.status[id] = s
	return nil
}

func staticService(b []Brand, tags map[int][]string) *Service {
	return &Service{load: func() ([]Brand, map[int][]string, error) { return b, tags, nil }}
}

func TestManagerGuardPausesNewBadCampaign(t *testing.T) {
	good := &models.Campaign{Base: models.Base{ID: 1}, Messenger: "email-ri", FromEmail: "a@rangeinsiders.com"}
	bad := &models.Campaign{Base: models.Base{ID: 2}, Messenger: "email-ri", FromEmail: "a@rangeinsiders.com"}
	running := &models.Campaign{Base: models.Base{ID: 3}, Messenger: "email-ri", FromEmail: "a@rangeinsiders.com"}
	f := &fakeStore{next: []*models.Campaign{good, bad, running}, status: map[int]string{}}
	lists := map[int][]int{1: {3}, 2: {3, 17}, 3: {3, 17}}
	svc := staticService(guardBrands(), guardTags)
	w := WrapManagerStore(f, svc, func(id int) ([]int, error) { return lists[id], nil }, log.New(io.Discard, "", 0))

	got, _ := w.NextCampaigns([]int64{3}, []int64{10})
	ids := []int{}
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	if !reflect.DeepEqual(ids, []int{1, 3}) {
		t.Fatalf("got %v", ids)
	}
	if f.status[2] != models.CampaignStatusPaused {
		t.Fatal("bad campaign not paused")
	}
	if _, ok := f.status[1]; ok {
		t.Fatal("good campaign touched")
	}
}

func TestGuardSender(t *testing.T) {
	svc := staticService(guardBrands(), guardTags)
	if err := svc.GuardSender([]int{3}, "email-ri", "RI <a@rangeinsiders.com>"); err != nil {
		t.Fatal(err)
	}
	if err := svc.GuardSender([]int{3}, "email-ta", "a@rangeinsiders.com"); err == nil {
		t.Fatal("wrong messenger accepted")
	}
	// Mixed brands: no single brand, so the rule-1 error.
	err := svc.GuardSender([]int{3, 17}, "email-ri", "a@rangeinsiders.com")
	if err == nil || strings.TrimSpace(err.Error()) == "" {
		t.Fatal("mixed-brand lists accepted")
	}
	if err := svc.GuardCampaign([]int{3, 17}, "email-ri", "a@rangeinsiders.com"); err == nil {
		t.Fatal("mixed-brand campaign accepted")
	}
}

func TestManagerGuardFirstDeployAndTagChange(t *testing.T) {
	db := testDB(t)
	db.MustExec(`INSERT INTO lists (uuid, name, type, optin, tags) VALUES (gen_random_uuid(), 'ri list', 'private', 'single', '{ri}')`)
	var lid int
	if err := db.Get(&lid, `SELECT MAX(id) FROM lists`); err != nil {
		t.Fatal(err)
	}
	db.MustExec(`INSERT INTO templates (name, type, subject, body) VALUES ('t', 'campaign', '', '{{ template "content" . }}')`)
	var tid int
	if err := db.Get(&tid, `SELECT MAX(id) FROM templates`); err != nil {
		t.Fatal(err)
	}
	mk := func(name, status string, sendAtPast bool) int {
		var id int
		sendAt := "NULL"
		if sendAtPast {
			sendAt = "NOW() - INTERVAL '1 hour'"
		}
		err := db.Get(&id, `INSERT INTO campaigns (uuid, name, subject, from_email, body, content_type, messenger, template_id, status, send_at)
			VALUES (gen_random_uuid(), $1, 's', 'RI <news@rangeinsiders.com>', 'b', 'richtext', 'email-ri', $2, $3, `+sendAt+`) RETURNING id`, name, tid, status)
		if err != nil {
			t.Fatal(err)
		}
		db.MustExec(`INSERT INTO campaign_lists (campaign_id, list_id, list_name) VALUES ($1, $2, 'x')`, id, lid)
		return id
	}
	runID := mk("running", "running", false)
	schedID := mk("scheduled", "scheduled", true)

	if err := Bootstrap(db); err != nil {
		t.Fatal(err)
	}
	if _, err := SeedInitial(db); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: NewStore(db)}
	run, sched := &models.Campaign{Base: models.Base{ID: runID}, Messenger: "email-ri", FromEmail: "RI <news@rangeinsiders.com>"},
		&models.Campaign{Base: models.Base{ID: schedID}, Messenger: "email-ri", FromEmail: "RI <news@rangeinsiders.com>"}
	listsFn := func(id int) ([]int, error) {
		var ids []int
		err := db.Select(&ids, `SELECT COALESCE(list_id, 0) FROM campaign_lists WHERE campaign_id = $1`, id)
		return ids, err
	}

	f := &fakeStore{next: []*models.Campaign{run, sched}, status: map[int]string{}}
	got, err := WrapManagerStore(f, svc, listsFn, log.New(io.Discard, "", 0)).NextCampaigns(nil, nil)
	if err != nil || len(got) != 2 || len(f.status) != 0 {
		t.Fatalf("first deploy: got %d campaigns, err %v, paused %v", len(got), err, f.status)
	}

	// The list is retagged to another brand: the new campaign is paused.
	db.MustExec(`UPDATE lists SET tags = '{ta}' WHERE id = $1`, lid)
	f = &fakeStore{next: []*models.Campaign{run, sched}, status: map[int]string{}}
	got, _ = WrapManagerStore(f, svc, listsFn, log.New(io.Discard, "", 0)).NextCampaigns(nil, nil)
	if len(got) != 0 || f.status[runID] != models.CampaignStatusPaused || f.status[schedID] != models.CampaignStatusPaused {
		t.Fatalf("retag: got %d, status %v", len(got), f.status)
	}
}

func TestManagerGuardLookupErrorHasOwnLogPrefix(t *testing.T) {
	c := &models.Campaign{Base: models.Base{ID: 5}, Messenger: "email-ri", FromEmail: "a@rangeinsiders.com"}
	f := &fakeStore{next: []*models.Campaign{c}, status: map[int]string{}}
	svc := staticService(guardBrands(), guardTags)
	var buf strings.Builder
	w := WrapManagerStore(f, svc, func(int) ([]int, error) { return nil, errors.New("db down") }, log.New(&buf, "", 0))

	got, _ := w.NextCampaigns(nil, nil)
	if len(got) != 0 || f.status[5] != models.CampaignStatusPaused {
		t.Fatal("lookup error must fail closed and pause")
	}
	if !strings.Contains(buf.String(), "send guard lookup error") || strings.Contains(buf.String(), "send guard paused") {
		t.Fatalf("log: %q", buf.String())
	}

	// A rule failure keeps the rule prefix.
	buf.Reset()
	w = WrapManagerStore(f, svc, func(int) ([]int, error) { return []int{3, 17}, nil }, log.New(&buf, "", 0))
	w.NextCampaigns(nil, nil)
	if !strings.Contains(buf.String(), "send guard paused") {
		t.Fatalf("log: %q", buf.String())
	}
}
