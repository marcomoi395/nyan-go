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
	deletes  *DeleteConfirmer
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
	deletes, err := NewDeleteConfirmer(service, clock)
	if err != nil {
		return nil, err
	}
	return &Orchestrator{provider: provider, ledger: service, clock: clock, deletes: deletes, pending: map[string]pendingRequest{}}, nil
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
	mutationPhase := false
	dispatch := func(callCtx context.Context, call FunctionCall) (string, error) {
		if mutationPhase && toolMutationKind(call.Name) == "" {
			return "", errors.New("read-only tool calls must precede mutations")
		}
		if toolMutationKind(call.Name) != "" {
			mutationPhase = true
		}
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

func (o *Orchestrator) ConfirmDelete(ctx context.Context, request ledger.RequestContext, token string) ([]ledger.Transaction, error) {
	return o.deletes.Confirm(ctx, request, token)
}

func (o *Orchestrator) dispatch(ctx context.Context, request ledger.RequestContext, call FunctionCall) (string, Mutation, bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &raw); err != nil {
		return "", Mutation{}, false, fmt.Errorf("tool %s arguments: %w", call.Name, err)
	}
	if err := validateToolFields(call.Name, raw); err != nil {
		return "", Mutation{}, false, err
	}
	getString := func(key string) string { var v string; _ = json.Unmarshal(raw[key], &v); return v }
	switch call.Name {
	case "create_transaction":
		return "", Mutation{Kind: MutationCreate, Input: ledger.TransactionInput{Type: ledger.TransactionType(getString("type")), AmountVND: ledger.AmountVND(number(raw["amount_vnd"])), Category: ledger.Category(getString("category")), Note: getString("note"), OccurredAt: parseTime(getString("occurred_at"))}}, true, nil
	case "update_transaction":
		return "", Mutation{Kind: MutationUpdate, ID: int64(number(raw["transaction_id"])), Input: ledger.TransactionInput{Type: ledger.TransactionType(getString("type")), AmountVND: ledger.AmountVND(number(raw["amount_vnd"])), Category: ledger.Category(getString("category")), Note: getString("note"), OccurredAt: parseTime(getString("occurred_at"))}}, true, nil
	case "delete_transaction":
		confirmation, err := o.deletes.Prepare(request, []int64{number(raw["transaction_id"])}, "giao dịch")
		if err != nil {
			return "", Mutation{}, false, err
		}
		return "Xác nhận xóa giao dịch trong 5 phút: delete:" + confirmation.Token, Mutation{}, false, nil
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

func validateToolFields(name string, raw map[string]json.RawMessage) error {
	allowed := map[string]map[string]string{
		"create_transaction": {"type": "string!", "amount_vnd": "int!", "category": "string!", "note": "string!", "occurred_at": "time!"},
		"update_transaction": {"transaction_id": "int!", "type": "string!", "amount_vnd": "int!", "category": "string!", "note": "string!", "occurred_at": "time!"},
		"delete_transaction": {"transaction_id": "int!"}, "restore_transaction": {"transaction_id": "int!"},
		"search_transactions": {"start": "time", "end": "time", "type": "string", "category": "string", "note": "string", "min_amount_vnd": "int", "max_amount_vnd": "int"},
		"get_statistics":      {"start": "time!", "end": "time!", "type": "string", "category": "string", "grouping": "string!", "comparison_start": "time", "comparison_end": "time"},
		"export_transactions": {"start": "time", "end": "time", "type": "string", "category": "string"},
	}
	schema, ok := allowed[name]
	if !ok {
		return fmt.Errorf("unsupported tool %q", name)
	}
	for key := range raw {
		if _, ok := schema[key]; !ok {
			return fmt.Errorf("unsupported field %q for %s", key, name)
		}
	}
	for key, kind := range schema {
		value, present := raw[key]
		required := strings.HasSuffix(kind, "!")
		kind = strings.TrimSuffix(kind, "!")
		if !present {
			if required {
				return fmt.Errorf("missing required field %q", key)
			}
			continue
		}
		switch kind {
		case "string":
			var v string
			if err := json.Unmarshal(value, &v); err != nil || required && strings.TrimSpace(v) == "" {
				return fmt.Errorf("invalid field %q", key)
			}
		case "int":
			var v int64
			if err := json.Unmarshal(value, &v); err != nil || v <= 0 {
				return fmt.Errorf("invalid field %q", key)
			}
		case "time":
			var v string
			if err := json.Unmarshal(value, &v); err != nil || required && strings.TrimSpace(v) == "" {
				return fmt.Errorf("invalid field %q", key)
			}
			if strings.TrimSpace(v) != "" {
				if _, err := time.Parse(time.RFC3339, v); err != nil {
					return fmt.Errorf("invalid field %q: %w", key, err)
				}
			}
		}
	}
	return nil
}
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
