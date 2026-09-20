package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nyan-go/internal/ledger"
	"nyan-go/internal/storage/sqlite"
)

func serviceFixture(t *testing.T) (*LedgerService, *sqlite.Store, ledger.RequestContext) {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	location, err := time.LoadLocation(ApplicationTimezone)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewLedgerService(store, location)
	if err != nil {
		t.Fatal(err)
	}
	return service, store, ledger.RequestContext{UserID: "u", GuildID: "g", ChannelID: "c", SourceMessageID: "m", ReceivedAt: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
}

func testExpense(at time.Time, amount ledger.AmountVND) ledger.TransactionInput {
	return ledger.TransactionInput{Type: ledger.TransactionExpense, AmountVND: amount, Category: ledger.CategoryFood, Note: "phở", OccurredAt: at}
}

func TestApplyMutationsAtomicAndIdempotent(t *testing.T) {
	service, store, request := serviceFixture(t)
	at := time.Date(2026, 9, 20, 4, 0, 0, 0, time.UTC)
	created, err := service.ApplyMutations(context.Background(), request, []Mutation{{Kind: MutationCreate, Input: testExpense(at, 45_000)}, {Kind: MutationCreate, Input: testExpense(at, 30_000)}})
	if err != nil || len(created) != 2 {
		t.Fatalf("create: %v %#v", err, created)
	}
	replayed, err := service.ApplyMutations(context.Background(), request, []Mutation{{Kind: MutationCreate, Input: testExpense(at, 1)}})
	if err != nil || len(replayed) != 2 || replayed[0].ID != created[0].ID {
		t.Fatalf("replay: %v %#v", err, replayed)
	}
	failedRequest := request
	failedRequest.SourceMessageID = "failed"
	if result, err := service.ApplyMutations(context.Background(), failedRequest, []Mutation{{Kind: MutationCreate, Input: testExpense(at, 1)}, {Kind: MutationUpdate, ID: 9999, Input: testExpense(at, 2)}}); err == nil || result != nil {
		t.Fatalf("expected rollback: %v %#v", err, result)
	}
	search, err := store.Search(context.Background(), sqlite.SearchFilter{UserID: "u", GuildID: "g", ChannelID: "c"})
	if err != nil || search.Total != 2 {
		t.Fatalf("rollback changed rows: %v %#v", err, search)
	}
}

func TestResolveUniqueAndStatisticsTimezone(t *testing.T) {
	service, _, request := serviceFixture(t)
	if _, err := service.ResolveUnique(context.Background(), request, SearchRequest{}, "sửa"); err == nil {
		t.Fatal("expected missing target clarification")
	}
	at := time.Date(2026, 9, 19, 17, 0, 0, 0, time.UTC) // Sep 20 00:00 local.
	if _, err := service.ApplyMutations(context.Background(), request, []Mutation{{Kind: MutationCreate, Input: testExpense(at, 45_000)}}); err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation(ApplicationTimezone)
	start := time.Date(2026, 9, 20, 0, 0, 0, 0, location)
	end := start.AddDate(0, 0, 1)
	stats, err := service.Statistics(context.Background(), request, StatisticsRequest{Start: start, End: end, Group: "day"})
	if err != nil || stats.TransactionCount != 1 || stats.TotalExpense != 45_000 || stats.Timezone != ApplicationTimezone {
		t.Fatalf("stats: %v %#v", err, stats)
	}
}

func TestExportCSVExcludesDeleted(t *testing.T) {
	service, _, request := serviceFixture(t)
	at := time.Date(2026, 9, 20, 4, 0, 0, 0, time.UTC)
	created, err := service.ApplyMutations(context.Background(), request, []Mutation{{Kind: MutationCreate, Input: testExpense(at, 45_000)}, {Kind: MutationCreate, Input: testExpense(at, 30_000)}})
	if err != nil {
		t.Fatal(err)
	}
	deleteRequest := request
	deleteRequest.SourceMessageID = "delete"
	if _, err := service.ApplyMutations(context.Background(), deleteRequest, []Mutation{{Kind: MutationDelete, ID: created[0].ID}}); err != nil {
		t.Fatal(err)
	}
	data, err := service.ExportCSV(context.Background(), request, SearchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "\n") != 2 || !strings.Contains(text, "30000") || strings.Contains(text, "45000") {
		t.Fatalf("unexpected CSV: %q", text)
	}
}

func TestConfirmMutationsVietnameseAndAmountFormatting(t *testing.T) {
	text := ConfirmMutations([]ledger.Transaction{{Type: ledger.TransactionExpense, AmountVND: 1_200_000, Category: ledger.CategoryFood, Note: "trưa"}})
	if !strings.Contains(text, "Đã ghi giao dịch") || !strings.Contains(text, "1.200.000 VND") {
		t.Fatalf("confirmation: %q", text)
	}
}

func TestExecuteCallsReadThenAtomicMutationBatch(t *testing.T) {
	service, _, request := serviceFixture(t)
	args, _ := json.Marshal(callCreateArgs{Type: ledger.TransactionExpense, AmountVND: 45_000, Category: ledger.CategoryFood, Note: "phở", OccurredAt: "2026-09-20"})
	results, err := service.ExecuteCalls(context.Background(), request, []FunctionCall{
		{Name: "search_transactions", Arguments: json.RawMessage(`{"note_contains":"none"}`)},
		{Name: "create_transaction", Arguments: args},
		{Name: "create_transaction", Arguments: args},
	})
	if err != nil || len(results) != 2 || len(results[1].Transactions) != 2 {
		t.Fatalf("execute calls: %v %#v", err, results)
	}
	if _, err := service.ExecuteCalls(context.Background(), request, []FunctionCall{{Name: "create_transaction", Arguments: args}, {Name: "search_transactions", Arguments: json.RawMessage(`{}`)}}); err == nil {
		t.Fatal("expected read-after-write rejection")
	}
}
