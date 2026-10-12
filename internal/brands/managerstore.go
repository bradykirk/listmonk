package brands

import (
	"errors"
	"log"

	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/models"
)

func (s *Service) loadAll() ([]Brand, map[int][]string, error) {
	if s.load != nil {
		return s.load()
	}
	b, err := s.Store.All()
	if err != nil {
		return nil, nil, err
	}
	t, err := s.Store.ListTags()
	return b, t, err
}

// GuardCampaign applies all send guard rules to a campaign about to send.
func (s *Service) GuardCampaign(listIDs []int, messenger, fromEmail string) error {
	b, t, err := s.loadAll()
	if err != nil {
		return err
	}
	return Check(GuardInput{ListIDs: listIDs, Messenger: messenger, FromEmail: fromEmail}, t, b)
}

// GuardSender applies the messenger and from-address rules (2-3) for the
// campaign's brand. A campaign with no single brand returns the rule-1 error.
func (s *Service) GuardSender(listIDs []int, messenger, fromEmail string) error {
	b, t, err := s.loadAll()
	if err != nil {
		return err
	}
	slug, reason := CampaignBrand(listIDs, t, b)
	if slug == "" {
		return errors.New(reason)
	}
	return CheckSender(slug, messenger, fromEmail, b)
}

type guardedStore struct {
	manager.Store
	svc   *Service
	lists func(int) ([]int, error)
	log   *log.Logger
}

// WrapManagerStore guards campaigns the manager picks up for the first time.
// A campaign that fails the guard is paused through the upstream status path.
func WrapManagerStore(inner manager.Store, svc *Service, campaignLists func(id int) ([]int, error), lo *log.Logger) manager.Store {
	return &guardedStore{Store: inner, svc: svc, lists: campaignLists, log: lo}
}

func (g *guardedStore) NextCampaigns(cur, sent []int64) ([]*models.Campaign, error) {
	out, err := g.Store.NextCampaigns(cur, sent)
	if err != nil {
		return out, err
	}
	running := make(map[int64]bool, len(cur))
	for _, id := range cur {
		running[id] = true
	}
	kept := make([]*models.Campaign, 0, len(out))
	for _, c := range out {
		// The manager already runs this campaign; it was checked when it started.
		if running[int64(c.ID)] {
			kept = append(kept, c)
			continue
		}
		ids, err := g.lists(c.ID)
		if err == nil {
			err = g.svc.GuardCampaign(ids, c.Messenger, c.FromEmail)
		}
		if err != nil {
			g.log.Printf("brands: send guard paused campaign %d (%s): %v", c.ID, c.Name, err)
			if perr := g.Store.UpdateCampaignStatus(c.ID, models.CampaignStatusPaused); perr != nil {
				g.log.Printf("brands: error pausing campaign %d: %v", c.ID, perr)
			}
			continue
		}
		kept = append(kept, c)
	}
	return kept, nil
}
