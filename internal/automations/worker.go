package automations

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// Marker is the unreplaced sender-info placeholder. A message that still contains it is not sent.
const Marker = "[[SENDER_INFO]]"

const (
	staleAfter     = 10 * time.Minute
	intentMinAge   = 2 * time.Minute
	intentMaxAge   = 7 * 24 * time.Hour
	markerDefer    = 10 * time.Minute
	markerLogEvery = time.Hour
	maxPerTick     = 100
)

// Message is one rendered step email for one subscriber.
type Message interface {
	Content() string // subject + "\n" + HTML body + "\n" + alt body, after the template is applied
	Send() error     // synchronous; nil means the messenger accepted it
}

// Deps are the functions the worker needs from the rest of the app.
type Deps struct {
	// Blocklisted reports the subscriber status.
	Blocklisted func(subscriberID int) (bool, error)
	// Guard runs brand send guard rules 1-4 for the step campaign.
	Guard func(campaignID int) error
	// Render loads the campaign and the subscriber and renders the message.
	Render func(campaignID, subscriberID int) (Message, error)
}

// Worker sends the due automation steps.
type Worker struct {
	store        *Store
	deps         Deps
	log          *log.Logger
	markerLogged map[string]time.Time
	now          func() time.Time
}

// NewWorker returns a worker that uses the store and the deps.
func NewWorker(s *Store, d Deps, lo *log.Logger) *Worker {
	return &Worker{store: s, deps: d, log: lo, markerLogged: map[string]time.Time{}, now: time.Now}
}

// Tick does one pass: mark stale sends, sweep intents, then process due enrollments.
func (w *Worker) Tick() {
	if _, err := w.store.MarkStale(staleAfter); err != nil {
		w.log.Printf("automations: mark stale: %v", err)
	}
	if _, err := w.store.SweepIntents(intentMinAge, intentMaxAge); err != nil {
		w.log.Printf("automations: sweep intents: %v", err)
	}
	for i := 0; i < maxPerTick; i++ {
		c, err := w.store.ClaimOne()
		if err != nil {
			w.log.Printf("automations: claim: %v", err)
			return
		}
		if c == nil {
			return
		}
		w.process(c)
	}
}

// Run ticks at start and then every interval, until ctx is done.
func (w *Worker) Run(ctx context.Context, every time.Duration) {
	w.Tick()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Tick()
		}
	}
}

func (w *Worker) check(op string, err error) {
	if err != nil {
		w.log.Printf("automations: %s: %v", op, err)
	}
}

func (w *Worker) process(c *Claimed) {
	s := w.store
	if !c.Enabled {
		w.check("finish", s.Finish(c.ID, "skipped", "automation disabled"))
		return
	}
	if !c.HasStep {
		w.check("finish", s.Finish(c.ID, "skipped", "no step"))
		return
	}

	blocked, err := w.deps.Blocklisted(c.SubscriberID)
	if err != nil {
		w.check("retry", s.Retry(c, err.Error()))
		return
	}
	if blocked {
		w.check("finish", s.Finish(c.ID, "skipped", "blocklisted"))
		return
	}
	active, err := s.ActiveOn(c.SubscriberID, c.ListID)
	if err != nil {
		w.check("retry", s.Retry(c, err.Error()))
		return
	}
	if !active {
		w.check("finish", s.Finish(c.ID, "skipped", "not active"))
		return
	}

	if err := w.deps.Guard(c.CampaignID); err != nil {
		w.log.Printf("automations: guard blocked step %d/%d: %v", c.AutomationID, c.Position, err)
		w.check("finish", s.Finish(c.ID, "skipped", "guard: "+err.Error()))
		return
	}

	msg, err := w.deps.Render(c.CampaignID, c.SubscriberID)
	if err != nil {
		w.check("retry", s.Retry(c, "render: "+err.Error()))
		return
	}

	if strings.Contains(msg.Content(), Marker) {
		key := fmt.Sprintf("%d/%d", c.AutomationID, c.Position)
		if last, ok := w.markerLogged[key]; !ok || w.now().Sub(last) > markerLogEvery {
			w.log.Printf("automations: marker in step %d/%d", c.AutomationID, c.Position)
			w.markerLogged[key] = w.now()
		}
		w.check("defer", s.Defer(c, markerDefer, "sender info marker"))
		return
	}

	if err := msg.Send(); err != nil {
		w.check("retry", s.Retry(c, err.Error()))
		return
	}
	w.check("advance", s.Advance(c))
}
