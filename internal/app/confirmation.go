package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"nyan-go/internal/ledger"
)

type DeleteConfirmation struct {
	Token     string
	ExpiresAt time.Time
	Count     int
	Summary   string
}
type DeleteConfirmer struct {
	service *LedgerService
	clock   Clock
	mu      sync.Mutex
	pending map[string]deleteRequest
}
type deleteRequest struct {
	request ledger.RequestContext
	ids     []int64
	expires time.Time
}

func NewDeleteConfirmer(service *LedgerService, clock Clock) (*DeleteConfirmer, error) {
	if service == nil {
		return nil, errors.New("ledger service is required")
	}
	if clock == nil {
		clock = realClock{}
	}
	return &DeleteConfirmer{service: service, clock: clock, pending: map[string]deleteRequest{}}, nil
}
func (c *DeleteConfirmer) Prepare(request ledger.RequestContext, ids []int64, summary string) (DeleteConfirmation, error) {
	if err := request.Validate(); err != nil {
		return DeleteConfirmation{}, err
	}
	if len(ids) == 0 {
		return DeleteConfirmation{}, errors.New("delete target is required")
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return DeleteConfirmation{}, err
	}
	token := hex.EncodeToString(tokenBytes)
	expires := c.clock.Now().Add(5 * time.Minute)
	c.mu.Lock()
	c.pending[token] = deleteRequest{request: request, ids: append([]int64(nil), ids...), expires: expires}
	c.mu.Unlock()
	return DeleteConfirmation{Token: token, ExpiresAt: expires, Count: len(ids), Summary: summary}, nil
}
func (c *DeleteConfirmer) Confirm(ctx context.Context, request ledger.RequestContext, token string) ([]ledger.Transaction, error) {
	c.mu.Lock()
	pending, ok := c.pending[token]
	if ok {
		delete(c.pending, token)
	}
	c.mu.Unlock()
	if !ok || c.clock.Now().After(pending.expires) || pending.request.UserID != request.UserID || pending.request.GuildID != request.GuildID || pending.request.ChannelID != request.ChannelID {
		return nil, errors.New("delete confirmation expired or unauthorized")
	}
	request.SourceMessageID = request.SourceMessageID + ":delete:" + token
	return c.service.DeleteTransactions(ctx, request, pending.ids)
}

type FileSender interface {
	SendFile(context.Context, string, string, string, io.Reader) error
}

func (s *LedgerService) ExportTempFile(ctx context.Context, request ledger.RequestContext, input SearchRequest) (string, error) {
	data, err := s.ExportCSV(ctx, request, input)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "nyan-go-export-*.csv")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}
