package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nyan-go/internal/ledger"
)

type DispatcherProvider interface {
	RespondWithDispatcher(context.Context, ProviderRequest, func(context.Context, FunctionCall) (string, error)) (ProviderResponse, error)
}

type Orchestrator struct {
	provider AIProvider
	ledger   *LedgerService
	clock    Clock
	mu       sync.Mutex
	pending  map[string]pendingRequest
}
type pendingRequest struct {
	message string
	expires time.Time
}

func NewOrchestrator(provider AIProvider, service *LedgerService, clock Clock) (*Orchestrator, error) {
	if provider == nil || service == nil {
		return nil, errors.New("orchestrator dependencies are required")
	}
	if clock == nil {
		clock = realClock{}
	}
	return &Orchestrator{provider: provider, ledger: service, clock: clock, pending: map[string]pendingRequest{}}, nil
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (o *Orchestrator) Handle(ctx context.Context, request ledger.RequestContext, message string) (string, error) {
	if err := request.Validate(); err != nil {
		return "", err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if strings.EqualFold(strings.TrimSpace(message), "hủy") || strings.EqualFold(strings.TrimSpace(message), "huy") {
		delete(o.pending, request.UserID)
		return "Đã hủy yêu cầu đang chờ.", nil
	}
	if pending, ok := o.pending[request.UserID]; ok && o.clock.Now().Before(pending.expires) {
		message = pending.message + "\n" + message
	} else if ok {
		delete(o.pending, request.UserID)
	}
	var response ProviderResponse
	var err error
	mutations := make([]Mutation, 0)
	toolMessages := make([]string, 0)
	dispatch := func(callCtx context.Context, call FunctionCall) (string, error) {
		result, mutation, isMutation, dispatchErr := o.dispatch(callCtx, request, call)
		if dispatchErr != nil {
			return "", dispatchErr
		}
		if isMutation {
			mutations = append(mutations, mutation)
			return `{"queued":true}`, nil
		}
		if strings.TrimSpace(result) != "" {
			toolMessages = append(toolMessages, result)
		}
		return result, nil
	}
	if continuation, ok := o.provider.(DispatcherProvider); ok {
		response, err = continuation.RespondWithDispatcher(ctx, ProviderRequest{Message: message}, dispatch)
	} else {
		response, err = o.provider.Respond(ctx, ProviderRequest{Message: message})
		if err == nil {
			for _, call := range response.FunctionCalls {
				if _, e := dispatch(ctx, call); e != nil {
					err = e
					break
				}
			}
		}
	}
	if err != nil {
		var clarificationErr *ClarificationError
		if errors.As(err, &clarificationErr) {
			o.pending[request.UserID] = pendingRequest{message: message, expires: o.clock.Now().Add(10 * time.Minute)}
			return clarificationErr.Message, nil
		}
		return "", fmt.Errorf("AI request failed: %w", err)
	}
	if len(mutations) > 0 {
		transactions, applyErr := o.ledger.ApplyMutations(ctx, request, mutations)
		if applyErr != nil {
			return "", applyErr
		}
		return ConfirmMutations(transactions), nil
	}
	if response.NoAction && strings.TrimSpace(response.Text) == "" {
		return "", nil
	}
	if strings.TrimSpace(response.Text) == "" && len(response.FunctionCalls) > 0 {
		if len(toolMessages) > 0 {
			return strings.Join(toolMessages, "\n"), nil
		}
		return "Đã xử lý yêu cầu.", nil
	}
	return response.Text, nil
}

func (o *Orchestrator) dispatch(ctx context.Context, request ledger.RequestContext, call FunctionCall) (string, Mutation, bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &raw); err != nil {
		return "", Mutation{}, false, fmt.Errorf("tool %s arguments: %w", call.Name, err)
	}
	getString := func(key string) string { var v string; _ = json.Unmarshal(raw[key], &v); return v }
	switch call.Name {
	case "create_transaction":
		return "", Mutation{Kind: MutationCreate, Input: ledger.TransactionInput{Type: ledger.TransactionType(getString("type")), AmountVND: ledger.AmountVND(number(raw["amount_vnd"])), Category: ledger.Category(getString("category")), Note: getString("note"), OccurredAt: parseTime(getString("occurred_at"))}}, true, nil
	case "update_transaction":
		return "", Mutation{Kind: MutationUpdate, ID: int64(number(raw["transaction_id"])), Input: ledger.TransactionInput{Type: ledger.TransactionType(getString("type")), AmountVND: ledger.AmountVND(number(raw["amount_vnd"])), Category: ledger.Category(getString("category")), Note: getString("note"), OccurredAt: parseTime(getString("occurred_at"))}}, true, nil
	case "delete_transaction":
		return "Xác nhận xóa giao dịch trong 5 phút. Mã xác nhận: " + fmt.Sprint(number(raw["transaction_id"])), Mutation{}, false, nil
	case "restore_transaction":
		return "", Mutation{Kind: MutationRestore, ID: int64(number(raw["transaction_id"]))}, true, nil
	case "search_transactions":
		result, err := o.ledger.Search(ctx, request, SearchRequest{Start: parseTime(getString("start")), End: parseTime(getString("end")), Type: ledger.TransactionType(getString("type")), Category: ledger.Category(getString("category")), NoteContains: getString("note"), MinAmountVND: ledger.AmountVND(number(raw["min_amount_vnd"])), MaxAmountVND: ledger.AmountVND(number(raw["max_amount_vnd"]))})
		if err != nil {
			return "", Mutation{}, false, err
		}
		encoded, err := marshal(result)
		return encoded, Mutation{}, false, err
	case "get_statistics":
		result, err := o.ledger.Statistics(ctx, request, StatisticsRequest{Start: parseTime(getString("start")), End: parseTime(getString("end")), Type: ledger.TransactionType(getString("type")), Category: ledger.Category(getString("category")), Group: getString("grouping"), CompareStart: parseTime(getString("comparison_start")), CompareEnd: parseTime(getString("comparison_end"))})
		if err != nil {
			return "", Mutation{}, false, err
		}
		encoded, err := marshal(result)
		return encoded, Mutation{}, false, err
	case "export_transactions":
		result, err := o.ledger.ExportCSV(ctx, request, SearchRequest{Start: parseTime(getString("start")), End: parseTime(getString("end")), Type: ledger.TransactionType(getString("type")), Category: ledger.Category(getString("category"))})
		if err != nil {
			return "", Mutation{}, false, err
		}
		return string(result), Mutation{}, false, nil
	default:
		return "", Mutation{}, false, fmt.Errorf("unsupported tool %q", call.Name)
	}
}

func number(raw json.RawMessage) int64 { var n int64; _ = json.Unmarshal(raw, &n); return n }
func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}
func marshal(value any) (string, error) {
	encoded, err := json.Marshal(value)
	return string(encoded), err
}
