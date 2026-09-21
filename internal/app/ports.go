package app

import (
	"context"
	"encoding/json"
	"time"

	"nyan-go/internal/ledger"
)

// Clock makes application boundaries deterministic in tests.
type Clock interface{ Now() time.Time }

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type ProviderRequest struct {
	Message string
	Tools   []ToolDefinition
}

type FunctionCall struct {
	ID         string
	CallID     string
	ResponseID string
	Name       string
	Arguments  json.RawMessage
}

type ProviderResponse struct {
	ResponseID    string
	Text          string
	FunctionCalls []FunctionCall
	NoAction      bool
	Usage         *ProviderUsage
}

type ProviderUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type AIProvider interface {
	Respond(context.Context, ProviderRequest) (ProviderResponse, error)
}

type LedgerRepository interface {
	CreateBatch(context.Context, ledger.RequestContext, []ledger.TransactionInput) ([]ledger.Transaction, error)
}

// MessageSender is the application-facing Discord output seam. Discord types
// stay outside the application and ledger packages.
type MessageSender interface {
	SendMessage(context.Context, string, string) error
}
