package manager

import (
	"errors"
	"io"
	"log"
	"testing"

	"github.com/knadh/listmonk/models"
)

// FORK(hcm): tests for the synchronous send.

type fakeMsgr struct {
	name string
	last models.Message
	err  error
}

func (f *fakeMsgr) Name() string                { return f.name }
func (f *fakeMsgr) Push(m models.Message) error { f.last = m; return f.err }
func (f *fakeMsgr) Flush() error                { return nil }
func (f *fakeMsgr) Close() error                { return nil }

func newForkTestManager(f *fakeMsgr) *Manager {
	return &Manager{
		messengers: map[string]Messenger{f.name: f},
		cfg:        Config{UnsubHeader: true},
		log:        log.New(io.Discard, "", 0),
	}
}

func testCampMsg(messenger string) CampaignMessage {
	return CampaignMessage{
		Campaign:   &models.Campaign{UUID: "c-uuid", Messenger: messenger, Name: "n"},
		Subscriber: models.Subscriber{Base: models.Base{ID: 7}, UUID: "s-uuid"},
		to:         "x@example.com",
		subject:    "Hi",
		body:       []byte("b"),
		unsubURL:   "https://u",
	}
}

func TestSendCampaignMessage(t *testing.T) {
	f := &fakeMsgr{name: "email"}
	m := newForkTestManager(f)
	msg := testCampMsg("email")
	if err := m.SendCampaignMessage(msg); err != nil {
		t.Fatal(err)
	}
	if f.last.Headers.Get(models.EmailHeaderCampaignUUID) != "c-uuid" || f.last.Headers.Get(models.EmailHeaderSubscriberUUID) != "s-uuid" {
		t.Fatalf("headers: %v", f.last.Headers)
	}
	if f.last.Headers.Get("List-Unsubscribe") != "<https://u>" {
		t.Fatalf("unsub header: %v", f.last.Headers)
	}
	f.err = errors.New("smtp down")
	if err := m.SendCampaignMessage(msg); err == nil || err.Error() != "smtp down" {
		t.Fatalf("want push error, got %v", err)
	}
	if err := m.SendCampaignMessage(testCampMsg("nope")); !errors.Is(err, ErrUnknownMessenger) {
		t.Fatalf("want ErrUnknownMessenger, got %v", err)
	}
}
