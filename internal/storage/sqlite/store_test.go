package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"nyan-go/internal/ledger"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "ledger.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func testRequest(source string) ledger.RequestContext {
	return ledger.RequestContext{UserID: "user", GuildID: "guild", ChannelID: "channel", SourceMessageID: source, ReceivedAt: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
}

func testInput(amount ledger.AmountVND, category ledger.Category, occurredAt time.Time) ledger.TransactionInput {
	return ledger.TransactionInput{Type: ledger.TransactionExpense, AmountVND: amount, Category: category, Note: "test", OccurredAt: occurredAt}
}

func TestApplyBatchAtomicIdempotentAndSoftDelete(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	request := testRequest("message-1")
	occurred := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	created, err := store.ApplyBatch(ctx, request, []Mutation{{Kind: MutationCreate, Input: testInput(45_000, ledger.CategoryFood, occurred)}, {Kind: MutationCreate, Input: testInput(30_000, ledger.CategoryTransport, occurred)}})
	if err != nil || len(created) != 2 {
		t.Fatalf("create batch: %v %#v", err, created)
	}
	replayed, err := store.ApplyBatch(ctx, request, []Mutation{{Kind: MutationCreate, Input: testInput(999, ledger.CategoryFood, occurred)}})
	if err != nil || len(replayed) != 2 || replayed[0].ID != created[0].ID {
		t.Fatalf("idempotent replay: %v %#v", err, replayed)
	}
	failed, err := store.ApplyBatch(ctx, testRequest("message-2"), []Mutation{{Kind: MutationCreate, Input: testInput(1, ledger.CategoryFood, occurred)}, {Kind: MutationUpdate, ID: 99999, Input: testInput(2, ledger.CategoryFood, occurred)}})
	if err == nil || failed != nil {
		t.Fatalf("expected atomic failure: %v %#v", err, failed)
	}
	if _, err := store.ApplyBatch(ctx, testRequest("message-3"), []Mutation{{Kind: MutationDelete, ID: created[0].ID}}); err != nil {
		t.Fatal(err)
	}
	search, err := store.Search(ctx, SearchFilter{UserID: "user", GuildID: "guild", ChannelID: "channel"})
	if err != nil || search.Total != 1 {
		t.Fatalf("deleted search: %v %#v", err, search)
	}
	if _, err := store.ApplyBatch(ctx, testRequest("message-4"), []Mutation{{Kind: MutationRestore, ID: created[0].ID}}); err != nil {
		t.Fatal(err)
	}
	search, err = store.Search(ctx, SearchFilter{UserID: "user", GuildID: "guild", ChannelID: "channel"})
	if err != nil || search.Total != 2 {
		t.Fatalf("restored search: %v %#v", err, search)
	}
}

func TestSearchLimitAndStatisticsBoundaries(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 21; i++ {
		input := testInput(1, ledger.CategoryFood, start.Add(time.Duration(i)*time.Hour))
		if _, err := store.CreateBatch(ctx, testRequest("message-"+string(rune('a'+i))), []ledger.TransactionInput{input}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := store.Search(ctx, SearchFilter{UserID: "user", GuildID: "guild", ChannelID: "channel"})
	if err != nil || len(result.Transactions) != 20 || result.Total != 21 || !result.Truncated {
		t.Fatalf("search limit: %v %#v", err, result)
	}
	stats, err := store.Statistics(ctx, StatisticsFilter{UserID: "user", GuildID: "guild", ChannelID: "channel", Start: start, End: start.Add(24 * time.Hour), Group: "day"})
	if err != nil || stats.TransactionCount != 21 || stats.TotalExpense != 21 || len(stats.Groups) != 2 {
		t.Fatalf("statistics: %v %#v", err, stats)
	}
}

func TestSearchPaginationAndLocalDayGrouping(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		occurred := time.Date(2026, 9, 20, i, 0, 0, 0, time.UTC)
		if _, err := store.CreateBatch(ctx, testRequest("page-"+string(rune('a'+i))), []ledger.TransactionInput{testInput(ledger.AmountVND(i+1), ledger.CategoryFood, occurred)}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.Search(ctx, SearchFilter{UserID: "user", GuildID: "guild", ChannelID: "channel", Limit: 2, Offset: 2})
	if err != nil || len(page.Transactions) != 2 || page.Total != 5 || !page.Truncated || page.Transactions[0].AmountVND != 3 {
		t.Fatalf("pagination: %v %#v", err, page)
	}

	local, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 20, 0, 0, 0, 0, local)
	end := start.AddDate(0, 0, 2)
	if _, err := store.CreateBatch(ctx, testRequest("local-day"), []ledger.TransactionInput{
		testInput(10, ledger.CategoryFood, time.Date(2026, 9, 20, 16, 30, 0, 0, time.UTC)),
		testInput(20, ledger.CategoryFood, time.Date(2026, 9, 20, 17, 30, 0, 0, time.UTC)),
	}); err != nil {
		t.Fatal(err)
	}
	stats, err := store.Statistics(ctx, StatisticsFilter{UserID: "user", GuildID: "guild", ChannelID: "channel", Start: start, End: end, Group: "day"})
	if err != nil || len(stats.Groups) != 2 || stats.Groups[0].Key != "2026-09-20" || stats.Groups[1].Key != "2026-09-21" {
		t.Fatalf("local day grouping: %v %#v", err, stats.Groups)
	}
}
