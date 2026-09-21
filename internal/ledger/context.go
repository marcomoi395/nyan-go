package ledger

import (
	"errors"
	"strings"
	"time"
)

// RequestContext contains identity derived from the authorized Discord event.
// Model-controlled tool arguments intentionally have no fields from this type.
type RequestContext struct {
	UserID          string
	GuildID         string
	ChannelID       string
	SourceMessageID string
	ReceivedAt      time.Time
}

func (context RequestContext) Validate() error {
	if strings.TrimSpace(context.UserID) == "" || strings.TrimSpace(context.GuildID) == "" || strings.TrimSpace(context.ChannelID) == "" || strings.TrimSpace(context.SourceMessageID) == "" {
		return errors.New("user, guild, channel, and source message IDs are required")
	}
	if context.ReceivedAt.IsZero() {
		return errors.New("received-at timestamp is required")
	}
	return nil
}
