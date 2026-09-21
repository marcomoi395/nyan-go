package reminder

import (
	"context"
	"testing"
	"time"

	"nyan-go/internal/storage/sqlite"
)

type testClock struct{ now time.Time }

func (c testClock) Now() time.Time { return c.now }

type testStore struct {
	state     sqlite.ReminderState
	activity  int
	delivered int
}

func (s *testStore) ReminderState(context.Context, string) (sqlite.ReminderState, error) {
	return s.state, nil
}
func (s *testStore) RecordReminderDelivery(context.Context, string, string, time.Time) error {
	s.delivered++
	return nil
}
func (s *testStore) Search(context.Context, sqlite.SearchFilter) (sqlite.SearchResult, error) {
	return sqlite.SearchResult{Total: s.activity}, nil
}

type testSender struct{ sent int }

func (s *testSender) SendMessage(context.Context, string, string) error { s.sent++; return nil }

func TestCheckSendsOnceAtConfiguredLocalTime(t *testing.T) {
	loc, _ := time.LoadLocation(ApplicationTimezone)
	store, sender := &testStore{}, &testSender{}
	scheduler, err := NewScheduler(store, sender, Config{UserID: "u", GuildID: "g", ChannelID: "c", Location: loc}, testClock{now: time.Date(2026, 9, 20, 20, 30, 0, 0, loc)})
	if err != nil {
		t.Fatal(err)
	}
	sent, err := scheduler.Check(context.Background())
	if err != nil || !sent {
		t.Fatalf("sent=%v err=%v", sent, err)
	}
	if sender.sent != 1 || store.delivered != 1 {
		t.Fatalf("sender=%d delivered=%d", sender.sent, store.delivered)
	}
	sent, err = scheduler.Check(context.Background())
	if err != nil || sent {
		t.Fatalf("duplicate reminder sent=%v err=%v", sent, err)
	}
}
