package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"nyan-go/internal/ledger"
	"nyan-go/internal/storage/sqlite"
)

type orchestratorProvider struct {
	calls int
	call  FunctionCall
}

func (p *orchestratorProvider) Respond(context.Context, ProviderRequest) (ProviderResponse, error) {
	return ProviderResponse{}, nil
}

func (p *orchestratorProvider) RespondWithDispatcher(ctx context.Context, _ ProviderRequest, dispatch func(context.Context, FunctionCall) (string, error)) (ProviderResponse, error) {
	p.calls++
	if _, err := dispatch(ctx, p.call); err != nil {
		return ProviderResponse{}, err
	}
	return ProviderResponse{FunctionCalls: []FunctionCall{p.call}}, nil
}

func TestOrchestratorDefaultsReceiptTimeAndOptionalNote(t *testing.T) {
	service, store, request := serviceFixture(t)
	provider := &orchestratorProvider{call: FunctionCall{Name: "create_transaction", Arguments: json.RawMessage(`{"type":"expense","amount_vnd":69000,"category":"food"}`)}}
	orchestrator, err := NewOrchestrator(provider, service, fixedClock{at: request.ReceivedAt})
	if err != nil {
		t.Fatal(err)
	}
	text, err := orchestrator.Handle(context.Background(), request, "bun mam 69k")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "69.000 VND") || provider.calls != 1 {
		t.Fatalf("text=%q calls=%d", text, provider.calls)
	}
	result, err := store.Search(context.Background(), searchFilterFor(request))
	if err != nil || result.Total != 1 {
		t.Fatalf("search=%+v err=%v", result, err)
	}
	if !result.Transactions[0].OccurredAt.Equal(request.ReceivedAt) || result.Transactions[0].Note != "bun mam" {
		t.Fatalf("transaction=%+v", result.Transactions[0])
	}
}

func TestOrchestratorInfersFoodExpenseFromSubject(t *testing.T) {
	service, store, request := serviceFixture(t)
	provider := &orchestratorProvider{call: FunctionCall{Name: "create_transaction", Arguments: json.RawMessage(`{"amount_vnd":25000,"note":"bun rieu"}`)}}
	orchestrator, err := NewOrchestrator(provider, service, fixedClock{at: request.ReceivedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.Handle(context.Background(), request, "bun rieu 25k"); err != nil {
		t.Fatal(err)
	}
	result, err := store.Search(context.Background(), searchFilterFor(request))
	if err != nil || result.Total != 1 || result.Transactions[0].Type != ledger.TransactionExpense || result.Transactions[0].Category != ledger.CategoryFood {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestOrchestratorAsksForSubjectWhenOnlyAmountIsKnown(t *testing.T) {
	service, _, request := serviceFixture(t)
	provider := &orchestratorProvider{call: FunctionCall{Name: "create_transaction", Arguments: json.RawMessage(`{"type":"expense","amount_vnd":25000}`)}}
	orchestrator, err := NewOrchestrator(provider, service, fixedClock{at: request.ReceivedAt})
	if err != nil {
		t.Fatal(err)
	}
	text, err := orchestrator.Handle(context.Background(), request, "25k")
	if err != nil || !strings.Contains(text, "giao dịch này là gì") {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestOrchestratorSearchResultExposesOnlyModelFields(t *testing.T) {
	service, _, request := serviceFixture(t)
	created, err := service.ApplyMutations(context.Background(), request, []Mutation{{Kind: MutationCreate, Input: testExpense(request.ReceivedAt, 25_000)}})
	if err != nil {
		t.Fatal(err)
	}
	provider := &dispatcherCaptureProvider{call: FunctionCall{Name: "search_transactions", Arguments: json.RawMessage(`{}`)}}
	orchestrator, err := NewOrchestrator(provider, service, fixedClock{at: request.ReceivedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.Handle(context.Background(), request, "tìm giao dịch"); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"creator_user_id", "guild_id", "channel_id", "source_message_id", "created_at", "updated_at", "deleted_at"} {
		if strings.Contains(provider.output, forbidden) {
			t.Fatalf("output contains %q: %s", forbidden, provider.output)
		}
	}
	if !strings.Contains(provider.output, `"id":`+json.Number(stringifyInt(created[0].ID)).String()) {
		t.Fatalf("output=%s", provider.output)
	}
}

func TestParseExactVietnameseAmounts(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int64
	}{
		{`25k`, 25_000}, {`1tr2`, 1_200_000}, {`1.200.000d`, 1_200_000}, {`1 trieu 200`, 1_200_000}, {`50 nghin`, 50_000},
	} {
		got, ok := parseAmount(json.RawMessage(`"` + test.input + `"`))
		if !ok || got != test.want {
			t.Errorf("parseAmount(%q) = %d, %v; want %d, true", test.input, got, ok, test.want)
		}
	}
	if _, ok := parseAmount(json.RawMessage(`"tam 50k"`)); ok {
		t.Fatal("approximate amount accepted")
	}
	if _, ok := parseAmount(json.RawMessage(`"1..2k"`)); ok {
		t.Fatal("malformed separators accepted")
	}
}

func TestParseTimeOfDayAtCanonicalHour(t *testing.T) {
	location, _ := time.LoadLocation(ApplicationTimezone)
	receivedAt := time.Date(2026, 9, 20, 8, 0, 0, 0, location)
	parsed, err := parseOccurredAt("sáng", receivedAt)
	if err != nil || !parsed.Equal(receivedAt) {
		t.Fatalf("parsed=%v err=%v", parsed, err)
	}
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

func searchFilterFor(request ledger.RequestContext) sqlite.SearchFilter {
	return sqlite.SearchFilter{UserID: request.UserID, GuildID: request.GuildID, ChannelID: request.ChannelID}
}

type dispatcherCaptureProvider struct {
	call   FunctionCall
	output string
}

func (p *dispatcherCaptureProvider) Respond(context.Context, ProviderRequest) (ProviderResponse, error) {
	return ProviderResponse{}, nil
}

func (p *dispatcherCaptureProvider) RespondWithDispatcher(ctx context.Context, _ ProviderRequest, dispatch func(context.Context, FunctionCall) (string, error)) (ProviderResponse, error) {
	var err error
	p.output, err = dispatch(ctx, p.call)
	return ProviderResponse{Text: `{"facts":[],"observations":[],"limitations":[]}`}, err
}

func stringifyInt(value int64) string { return strconv.FormatInt(value, 10) }
