package ledger

import (
	"testing"
	"time"
)

func TestTransactionInputValidation(t *testing.T) {
	valid := TransactionInput{Type: TransactionExpense, AmountVND: 45_000, Category: CategoryFood, Note: "phở", OccurredAt: time.Unix(1, 0)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []TransactionInput{
		{Type: TransactionExpense, AmountVND: 0, Category: CategoryFood, OccurredAt: valid.OccurredAt},
		{Type: TransactionType("transfer"), AmountVND: 1, Category: CategoryOther, OccurredAt: valid.OccurredAt},
		{Type: TransactionIncome, AmountVND: 1, Category: CategoryFood, OccurredAt: valid.OccurredAt},
		{Type: TransactionExpense, AmountVND: 1, Category: CategoryFood},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("expected invalid input: %+v", invalid)
		}
	}
}

func TestDateRangeAndContextValidation(t *testing.T) {
	if err := (DateRange{Start: time.Unix(2, 0), End: time.Unix(1, 0)}).Validate(); err == nil {
		t.Fatal("expected invalid date range")
	}
	if err := (RequestContext{UserID: "u", GuildID: "g", ChannelID: "c", SourceMessageID: "m", ReceivedAt: time.Now()}).Validate(); err != nil {
		t.Fatal(err)
	}
}
