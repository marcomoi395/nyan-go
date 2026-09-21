package ledger

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type TransactionType string

const (
	TransactionIncome  TransactionType = "income"
	TransactionExpense TransactionType = "expense"
)

type Category string

const (
	CategorySalary           Category = "salary"
	CategoryBonus            Category = "bonus"
	CategoryFreelance        Category = "freelance"
	CategoryBusiness         Category = "business"
	CategoryInvestmentReturn Category = "investment_return"
	CategoryRefund           Category = "refund"
	CategoryGift             Category = "gift"
	CategoryFood             Category = "food"
	CategoryTransport        Category = "transport"
	CategoryHousing          Category = "housing"
	CategoryUtilities        Category = "utilities"
	CategoryShopping         Category = "shopping"
	CategoryHealth           Category = "health"
	CategoryEducation        Category = "education"
	CategoryEntertainment    Category = "entertainment"
	CategoryTravel           Category = "travel"
	CategoryInsurance        Category = "insurance"
	CategoryTaxFee           Category = "tax_fee"
	CategoryFamily           Category = "family"
	CategoryPet              Category = "pet"
	CategoryWork             Category = "work"
	CategoryDebtFinance      Category = "debt_finance"
	CategoryOther            Category = "other"
)

const (
	MaxNoteLength = 2000
	MaxTextLength = 2000
)

var incomeCategories = map[Category]struct{}{
	CategorySalary: {}, CategoryBonus: {}, CategoryFreelance: {}, CategoryBusiness: {},
	CategoryInvestmentReturn: {}, CategoryRefund: {}, CategoryGift: {}, CategoryOther: {},
}

var expenseCategories = map[Category]struct{}{
	CategoryFood: {}, CategoryTransport: {}, CategoryHousing: {}, CategoryUtilities: {},
	CategoryShopping: {}, CategoryHealth: {}, CategoryEducation: {}, CategoryEntertainment: {},
	CategoryTravel: {}, CategoryInsurance: {}, CategoryTaxFee: {}, CategoryFamily: {},
	CategoryPet: {}, CategoryWork: {}, CategoryDebtFinance: {}, CategoryOther: {},
}

type AmountVND int64

func (amount AmountVND) Validate() error {
	if amount <= 0 {
		return errors.New("amount must be a positive integer VND value")
	}
	return nil
}

type DateRange struct {
	Start time.Time
	End   time.Time
}

func (r DateRange) Validate() error {
	if r.Start.IsZero() || r.End.IsZero() {
		return errors.New("date range boundaries are required")
	}
	if !r.Start.Before(r.End) {
		return errors.New("date range start must be before end")
	}
	return nil
}

type TransactionInput struct {
	Type       TransactionType
	AmountVND  AmountVND
	Category   Category
	Note       string
	OccurredAt time.Time
}

func (input TransactionInput) Validate() error {
	if err := input.AmountVND.Validate(); err != nil {
		return err
	}
	if err := ValidateTransactionType(input.Type); err != nil {
		return err
	}
	if err := ValidateCategory(input.Type, input.Category); err != nil {
		return err
	}
	if len([]rune(input.Note)) > MaxNoteLength {
		return fmt.Errorf("note exceeds %d characters", MaxNoteLength)
	}
	if input.OccurredAt.IsZero() {
		return errors.New("occurred-at timestamp is required")
	}
	return nil
}

type Transaction struct {
	ID              int64
	Type            TransactionType
	AmountVND       AmountVND
	Category        Category
	Note            string
	OccurredAt      time.Time
	CreatorUserID   string
	GuildID         string
	ChannelID       string
	SourceMessageID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

func ValidateTransactionType(value TransactionType) error {
	if value != TransactionIncome && value != TransactionExpense {
		return fmt.Errorf("unsupported transaction type %q", value)
	}
	return nil
}

func ValidateCategory(transactionType TransactionType, category Category) error {
	if err := ValidateTransactionType(transactionType); err != nil {
		return err
	}
	if strings.TrimSpace(string(category)) == "" {
		return errors.New("category is required")
	}
	set := incomeCategories
	if transactionType == TransactionExpense {
		set = expenseCategories
	}
	if _, ok := set[category]; !ok {
		return fmt.Errorf("unsupported category %q for %s", category, transactionType)
	}
	return nil
}

func IncomeCategories() []Category  { return categoryList(incomeCategories) }
func ExpenseCategories() []Category { return categoryList(expenseCategories) }

func categoryList(categories map[Category]struct{}) []Category {
	result := make([]Category, 0, len(categories))
	for category := range categories {
		result = append(result, category)
	}
	// Stable ordering keeps prompts and tests deterministic.
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j] < result[j-1]; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}
	return result
}
