package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type ReminderState struct {
	UserID           string
	LastDeliveryDate string
	LastDeliveryAt   *time.Time
}

func (s *Store) ReminderState(ctx context.Context, userID string) (ReminderState, error) {
	var state ReminderState
	var deliveryDate sql.NullString
	var deliveryAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT user_id, last_delivery_date, last_delivery_at FROM reminder_state WHERE user_id = ?`, userID).Scan(&state.UserID, &deliveryDate, &deliveryAt)
	if errorsIsNoRows(err) {
		return ReminderState{UserID: userID}, nil
	}
	if err != nil {
		return state, fmt.Errorf("read reminder state: %w", err)
	}
	if deliveryDate.Valid {
		state.LastDeliveryDate = deliveryDate.String
	}
	if deliveryAt.Valid {
		value, err := parseTime(deliveryAt.String)
		if err != nil {
			return state, err
		}
		state.LastDeliveryAt = &value
	}
	return state, nil
}

func (s *Store) RecordReminderDelivery(ctx context.Context, userID, localDate string, deliveredAt time.Time) error {
	if userID == "" || localDate == "" || deliveredAt.IsZero() {
		return fmt.Errorf("reminder delivery fields are required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO reminder_state(user_id, last_delivery_date, last_delivery_at) VALUES (?, ?, ?) ON CONFLICT(user_id) DO UPDATE SET last_delivery_date = excluded.last_delivery_date, last_delivery_at = excluded.last_delivery_at`, userID, localDate, formatTime(deliveredAt))
	if err != nil {
		return fmt.Errorf("record reminder delivery: %w", err)
	}
	return nil
}

func errorsIsNoRows(err error) bool { return err == sql.ErrNoRows }
