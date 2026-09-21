package reminder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nyan-go/internal/storage/sqlite"
)

const (
	ApplicationTimezone = "Asia/Ho_Chi_Minh"
	DefaultReminderTime = "20:30"
	DefaultMessage      = "Hôm nay bạn chưa ghi chép giao dịch. Nhắc bạn cập nhật sổ thu chi nhé!"
)

type Clock interface{ Now() time.Time }
type Store interface {
	ReminderState(context.Context, string) (sqlite.ReminderState, error)
	RecordReminderDelivery(context.Context, string, string, time.Time) error
	Search(context.Context, sqlite.SearchFilter) (sqlite.SearchResult, error)
}
type Sender interface {
	SendMessage(context.Context, string, string) error
}

type Config struct {
	UserID, GuildID, ChannelID string
	ReminderTime               string
	ReminderCooldown           time.Duration
	Location                   *time.Location
	Message                    string
}

type Scheduler struct {
	store     Store
	sender    Sender
	clock     Clock
	config    Config
	loc       *time.Location
	mu        sync.Mutex
	attempted map[string]struct{}
}

func NewScheduler(store Store, sender Sender, config Config, clock Clock) (*Scheduler, error) {
	if store == nil || sender == nil || clock == nil {
		return nil, errors.New("reminder dependencies are required")
	}
	for name, value := range map[string]string{"user ID": config.UserID, "guild ID": config.GuildID, "channel ID": config.ChannelID} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s is required", name)
		}
	}
	if config.ReminderTime == "" {
		config.ReminderTime = DefaultReminderTime
	}
	if _, err := time.Parse("15:04", config.ReminderTime); err != nil {
		return nil, fmt.Errorf("invalid reminder time: %w", err)
	}
	if config.ReminderCooldown <= 0 {
		config.ReminderCooldown = 24 * time.Hour
	}
	if config.Location == nil {
		var err error
		config.Location, err = time.LoadLocation(ApplicationTimezone)
		if err != nil {
			return nil, err
		}
	}
	if config.Message == "" {
		config.Message = DefaultMessage
	}
	return &Scheduler{store: store, sender: sender, clock: clock, config: config, loc: config.Location, attempted: map[string]struct{}{}}, nil
}

func (s *Scheduler) Check(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now().In(s.loc)
	target, _ := time.Parse("15:04", s.config.ReminderTime)
	if now.Hour() != target.Hour() || now.Minute() != target.Minute() {
		return false, nil
	}
	date := now.Format("2006-01-02")
	if _, ok := s.attempted[date]; ok {
		return false, nil
	}
	state, err := s.store.ReminderState(ctx, s.config.UserID)
	if err != nil {
		return false, err
	}
	if state.LastDeliveryDate == date || state.LastDeliveryAt != nil && now.Before(state.LastDeliveryAt.Add(s.config.ReminderCooldown)) {
		s.attempted[date] = struct{}{}
		return false, nil
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)
	activity, err := s.store.Search(ctx, sqlite.SearchFilter{UserID: s.config.UserID, GuildID: s.config.GuildID, ChannelID: s.config.ChannelID, Start: start, End: start.AddDate(0, 0, 1)})
	if err != nil {
		return false, err
	}
	if activity.Total > 0 {
		s.attempted[date] = struct{}{}
		return false, nil
	}
	s.attempted[date] = struct{}{}
	if err := s.sender.SendMessage(ctx, s.config.ChannelID, s.config.Message); err != nil {
		return false, err
	}
	if err := s.store.RecordReminderDelivery(ctx, s.config.UserID, date, now); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Scheduler) Run(ctx context.Context) error {
	for {
		now := s.clock.Now().In(s.loc)
		target, _ := time.Parse("15:04", s.config.ReminderTime)
		next := time.Date(now.Year(), now.Month(), now.Day(), target.Hour(), target.Minute(), 0, 0, s.loc)
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		timer := time.NewTimer(next.Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			_, _ = s.Check(ctx)
		}
	}
}
