package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"nyan-go/internal/ledger"
	"nyan-go/internal/storage/sqlite"
)

const (
	ApplicationTimezone = "Asia/Ho_Chi_Minh"
	Currency            = "VND"
)

// MutationKind is the model-facing operation set. Identity is deliberately
// absent; it is supplied by RequestContext at the Discord boundary.
type MutationKind string

const (
	MutationCreate  MutationKind = "create"
	MutationUpdate  MutationKind = "update"
	MutationDelete  MutationKind = "delete"
	MutationRestore MutationKind = "restore"
)

type Mutation struct {
	Kind  MutationKind
	ID    int64
	Input ledger.TransactionInput
}

// SearchRequest contains only user-controlled filters. Scope is always
// overwritten from the trusted request context.
type SearchRequest struct {
	Start        time.Time
	End          time.Time
	Type         ledger.TransactionType
	Category     ledger.Category
	NoteContains string
	MinAmountVND ledger.AmountVND
	MaxAmountVND ledger.AmountVND
}

type StatisticsRequest struct {
	Start        time.Time
	End          time.Time
	Type         ledger.TransactionType
	Category     ledger.Category
	Group        string
	CompareStart time.Time
	CompareEnd   time.Time
}

type ToolResult struct {
	Name         string
	Transactions []ledger.Transaction
	Search       *sqlite.SearchResult
	Statistics   *sqlite.Statistics
	CSV          []byte
}

type callCreateArgs struct {
	Type       ledger.TransactionType `json:"type"`
	AmountVND  ledger.AmountVND       `json:"amount_vnd"`
	Category   ledger.Category        `json:"category"`
	Note       string                 `json:"note"`
	OccurredAt string                 `json:"occurred_at"`
}
type callUpdateArgs struct {
	ID int64 `json:"id"`
	callCreateArgs
}
type callIDArgs struct {
	ID int64 `json:"id"`
}
type callSearchArgs struct {
	Start        string                 `json:"start"`
	End          string                 `json:"end"`
	Type         ledger.TransactionType `json:"type"`
	Category     ledger.Category        `json:"category"`
	NoteContains string                 `json:"note_contains"`
	MinAmountVND ledger.AmountVND       `json:"min_amount_vnd"`
	MaxAmountVND ledger.AmountVND       `json:"max_amount_vnd"`
}
type callStatisticsArgs struct {
	Start        string                 `json:"start"`
	End          string                 `json:"end"`
	Type         ledger.TransactionType `json:"type"`
	Category     ledger.Category        `json:"category"`
	Group        string                 `json:"group"`
	CompareStart string                 `json:"compare_start"`
	CompareEnd   string                 `json:"compare_end"`
}

func (args callSearchArgs) search(location *time.Location) (SearchRequest, error) {
	start, end, err := parseOptionalRange(args.Start, args.End, location)
	if err != nil {
		return SearchRequest{}, err
	}
	return SearchRequest{Start: start, End: end, Type: args.Type, Category: args.Category, NoteContains: args.NoteContains, MinAmountVND: args.MinAmountVND, MaxAmountVND: args.MaxAmountVND}, nil
}

func (args callStatisticsArgs) statistics(location *time.Location) (StatisticsRequest, error) {
	start, end, err := parseOptionalRange(args.Start, args.End, location)
	if err != nil {
		return StatisticsRequest{}, err
	}
	compareStart, compareEnd, err := parseOptionalRange(args.CompareStart, args.CompareEnd, location)
	if err != nil {
		return StatisticsRequest{}, err
	}
	return StatisticsRequest{Start: start, End: end, Type: args.Type, Category: args.Category, Group: args.Group, CompareStart: compareStart, CompareEnd: compareEnd}, nil
}

func parseOptionalRange(start, end string, location *time.Location) (time.Time, time.Time, error) {
	if strings.TrimSpace(start) == "" && strings.TrimSpace(end) == "" {
		return time.Time{}, time.Time{}, nil
	}
	if strings.TrimSpace(start) == "" || strings.TrimSpace(end) == "" {
		return time.Time{}, time.Time{}, clarification("Bạn hãy cho biết đủ ngày bắt đầu và ngày kết thúc.")
	}
	parse := func(value string) (time.Time, error) {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			parsed, err = time.ParseInLocation("2006-01-02", value, location)
		}
		return parsed.In(location), err
	}
	parsedStart, err := parse(start)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start time: %w", err)
	}
	parsedEnd, err := parse(end)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end time: %w", err)
	}
	return parsedStart, parsedEnd, nil
}

// ExecuteCalls dispatches read-only calls, then one ordered mutation batch.
// A read after a mutation or a second mutation phase is rejected.
func (s *LedgerService) ExecuteCalls(ctx context.Context, request ledger.RequestContext, calls []FunctionCall) ([]ToolResult, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if len(calls) == 0 {
		return nil, errors.New("tool call batch cannot be empty")
	}
	results := make([]ToolResult, 0, len(calls))
	mutations := make([]Mutation, 0)
	mutationPhase := false
	for _, call := range calls {
		kind := toolMutationKind(call.Name)
		if kind != "" {
			mutationPhase = true
			mutation, err := s.decodeMutation(call.Name, call.Arguments)
			if err != nil {
				return nil, err
			}
			mutations = append(mutations, mutation)
			continue
		}
		if mutationPhase {
			return nil, errors.New("read-only tool calls must precede mutations")
		}
		result, err := s.executeReadCall(ctx, request, call)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if len(mutations) > 0 {
		transactions, err := s.ApplyMutations(ctx, request, mutations)
		if err != nil {
			return nil, err
		}
		results = append(results, ToolResult{Name: "mutation_batch", Transactions: transactions})
	}
	return results, nil
}

func toolMutationKind(name string) MutationKind {
	switch name {
	case "create_transaction":
		return MutationCreate
	case "update_transaction":
		return MutationUpdate
	case "delete_transaction":
		return MutationDelete
	case "restore_transaction":
		return MutationRestore
	default:
		return ""
	}
}

func (s *LedgerService) decodeMutation(name string, raw json.RawMessage) (Mutation, error) {
	switch name {
	case "create_transaction":
		var args callCreateArgs
		if err := decodeArgs(raw, &args); err != nil {
			return Mutation{}, err
		}
		input, err := args.input(s.loc)
		return Mutation{Kind: MutationCreate, Input: input}, err
	case "update_transaction":
		var args callUpdateArgs
		if err := decodeArgs(raw, &args); err != nil {
			return Mutation{}, err
		}
		input, err := args.callCreateArgs.input(s.loc)
		return Mutation{Kind: MutationUpdate, ID: args.ID, Input: input}, err
	case "delete_transaction", "restore_transaction":
		var args callIDArgs
		if err := decodeArgs(raw, &args); err != nil {
			return Mutation{}, err
		}
		return Mutation{Kind: toolMutationKind(name), ID: args.ID}, nil
	default:
		return Mutation{}, fmt.Errorf("unsupported mutation tool %q", name)
	}
}

func (args callCreateArgs) input(location *time.Location) (ledger.TransactionInput, error) {
	occurredAt, err := parseToolTime(args.OccurredAt, location)
	if err != nil {
		return ledger.TransactionInput{}, err
	}
	return ledger.TransactionInput{Type: args.Type, AmountVND: args.AmountVND, Category: args.Category, Note: args.Note, OccurredAt: occurredAt}, nil
}

func decodeArgs(raw json.RawMessage, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("tool arguments are required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return nil
}

func parseToolTime(value string, location *time.Location) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, clarification("Bạn hãy cho biết ngày giao dịch trước khi lưu.")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		parsed, err = time.ParseInLocation("2006-01-02", value, location)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid occurred_at: %w", err)
	}
	return parsed.In(location), nil
}

func (s *LedgerService) executeReadCall(ctx context.Context, request ledger.RequestContext, call FunctionCall) (ToolResult, error) {
	switch call.Name {
	case "search_transactions":
		var args callSearchArgs
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return ToolResult{}, err
		}
		input, err := args.search(s.loc)
		if err != nil {
			return ToolResult{}, err
		}
		result, err := s.Search(ctx, request, input)
		return ToolResult{Name: call.Name, Search: &result}, err
	case "get_statistics":
		var args callStatisticsArgs
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return ToolResult{}, err
		}
		input, err := args.statistics(s.loc)
		if err != nil {
			return ToolResult{}, err
		}
		result, err := s.Statistics(ctx, request, input)
		return ToolResult{Name: call.Name, Statistics: &result}, err
	case "export_transactions":
		var args callSearchArgs
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return ToolResult{}, err
		}
		input, err := args.search(s.loc)
		if err != nil {
			return ToolResult{}, err
		}
		result, err := s.ExportCSV(ctx, request, input)
		return ToolResult{Name: call.Name, CSV: result}, err
	default:
		return ToolResult{}, fmt.Errorf("unsupported tool %q", call.Name)
	}
}

type ClarificationError struct{ Message string }

func (e *ClarificationError) Error() string { return e.Message }

func clarification(message string) error {
	return &ClarificationError{Message: message}
}

// LedgerService is the deterministic application seam above SQLite.
type LedgerService struct {
	store LedgerStore
	loc   *time.Location
}

type LedgerStore interface {
	ApplyBatch(context.Context, ledger.RequestContext, []sqlite.Mutation) ([]ledger.Transaction, error)
	Search(context.Context, sqlite.SearchFilter) (sqlite.SearchResult, error)
	Statistics(context.Context, sqlite.StatisticsFilter) (sqlite.Statistics, error)
}

type LedgerExporter interface {
	Export(context.Context, sqlite.SearchFilter) ([]ledger.Transaction, error)
}

type LedgerReader interface {
	Get(context.Context, ledger.RequestContext, int64) (ledger.Transaction, error)
}

func NewLedgerService(store LedgerStore, location *time.Location) (*LedgerService, error) {
	if store == nil {
		return nil, errors.New("ledger store is required")
	}
	if location == nil {
		var err error
		location, err = time.LoadLocation(ApplicationTimezone)
		if err != nil {
			return nil, fmt.Errorf("load application timezone: %w", err)
		}
	}
	return &LedgerService{store: store, loc: location}, nil
}

// ApplyMutations validates the complete ordered batch before handing it to
// SQLite, which then executes it in one transaction.
func (s *LedgerService) ApplyMutations(ctx context.Context, request ledger.RequestContext, mutations []Mutation) ([]ledger.Transaction, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if len(mutations) == 0 {
		return nil, errors.New("mutation batch cannot be empty")
	}
	converted := make([]sqlite.Mutation, len(mutations))
	for i, mutation := range mutations {
		kind, err := sqliteMutationKind(mutation.Kind)
		if err != nil {
			return nil, err
		}
		if mutation.Kind == MutationCreate || mutation.Kind == MutationUpdate {
			if err := mutation.Input.Validate(); err != nil {
				return nil, fmt.Errorf("mutation %d: %w", i+1, err)
			}
		}
		if (mutation.Kind == MutationUpdate || mutation.Kind == MutationDelete || mutation.Kind == MutationRestore) && mutation.ID <= 0 {
			return nil, clarification("Bạn cần cho biết giao dịch cụ thể để thực hiện thao tác này.")
		}
		converted[i] = sqlite.Mutation{Kind: kind, ID: mutation.ID, Input: mutation.Input}
	}
	return s.store.ApplyBatch(ctx, request, converted)
}

func sqliteMutationKind(kind MutationKind) (sqlite.MutationKind, error) {
	switch kind {
	case MutationCreate:
		return sqlite.MutationCreate, nil
	case MutationUpdate:
		return sqlite.MutationUpdate, nil
	case MutationDelete:
		return sqlite.MutationDelete, nil
	case MutationRestore:
		return sqlite.MutationRestore, nil
	default:
		return "", fmt.Errorf("unsupported mutation kind %q", kind)
	}
}

func (s *LedgerService) CreateBatch(ctx context.Context, request ledger.RequestContext, inputs []ledger.TransactionInput) ([]ledger.Transaction, error) {
	mutations := make([]Mutation, len(inputs))
	for i, input := range inputs {
		mutations[i] = Mutation{Kind: MutationCreate, Input: input}
	}
	return s.ApplyMutations(ctx, request, mutations)
}

func (s *LedgerService) UpdateTransaction(ctx context.Context, request ledger.RequestContext, id int64, input ledger.TransactionInput) (ledger.Transaction, error) {
	result, err := s.ApplyMutations(ctx, request, []Mutation{{Kind: MutationUpdate, ID: id, Input: input}})
	if err != nil {
		return ledger.Transaction{}, err
	}
	return result[0], nil
}

func (s *LedgerService) DeleteTransactions(ctx context.Context, request ledger.RequestContext, ids []int64) ([]ledger.Transaction, error) {
	mutations := make([]Mutation, len(ids))
	for i, id := range ids {
		mutations[i] = Mutation{Kind: MutationDelete, ID: id}
	}
	return s.ApplyMutations(ctx, request, mutations)
}

func (s *LedgerService) RestoreTransaction(ctx context.Context, request ledger.RequestContext, id int64) (ledger.Transaction, error) {
	result, err := s.ApplyMutations(ctx, request, []Mutation{{Kind: MutationRestore, ID: id}})
	if err != nil {
		return ledger.Transaction{}, err
	}
	return result[0], nil
}

func (s *LedgerService) Search(ctx context.Context, request ledger.RequestContext, input SearchRequest) (sqlite.SearchResult, error) {
	if err := request.Validate(); err != nil {
		return sqlite.SearchResult{}, err
	}
	filter, err := s.searchFilter(request, input)
	if err != nil {
		return sqlite.SearchResult{}, err
	}
	return s.store.Search(ctx, filter)
}

func (s *LedgerService) GetTransaction(ctx context.Context, request ledger.RequestContext, id int64) (ledger.Transaction, error) {
	if err := request.Validate(); err != nil {
		return ledger.Transaction{}, err
	}
	reader, ok := s.store.(LedgerReader)
	if !ok {
		return ledger.Transaction{}, errors.New("ledger store does not support transaction reads")
	}
	return reader.Get(ctx, request, id)
}

func (s *LedgerService) Statistics(ctx context.Context, request ledger.RequestContext, input StatisticsRequest) (sqlite.Statistics, error) {
	if err := request.Validate(); err != nil {
		return sqlite.Statistics{}, err
	}
	if input.Start.IsZero() || input.End.IsZero() {
		return sqlite.Statistics{}, clarification("Bạn muốn xem thống kê trong khoảng thời gian nào? Hãy cho biết ngày bắt đầu và ngày kết thúc.")
	}
	if !input.Start.Before(input.End) {
		return sqlite.Statistics{}, clarification("Khoảng thời gian thống kê chưa hợp lệ; ngày bắt đầu phải trước ngày kết thúc.")
	}
	if (input.CompareStart.IsZero() != input.CompareEnd.IsZero()) || (!input.CompareStart.IsZero() && !input.CompareStart.Before(input.CompareEnd)) {
		return sqlite.Statistics{}, clarification("Khoảng so sánh chưa hợp lệ; hãy cho biết hai mốc thời gian theo đúng thứ tự.")
	}
	if err := validateFilters(input.Type, input.Category, 0, 0); err != nil {
		return sqlite.Statistics{}, err
	}
	filter := sqlite.StatisticsFilter{
		UserID: request.UserID, GuildID: request.GuildID, ChannelID: request.ChannelID,
		Start: s.local(input.Start), End: s.local(input.End), Type: input.Type, Category: input.Category, Group: input.Group,
		CompareStart: localIfSet(input.CompareStart, s.loc), CompareEnd: localIfSet(input.CompareEnd, s.loc),
	}
	return s.store.Statistics(ctx, filter)
}

func (s *LedgerService) searchFilter(request ledger.RequestContext, input SearchRequest) (sqlite.SearchFilter, error) {
	filter := sqlite.SearchFilter{
		UserID: request.UserID, GuildID: request.GuildID, ChannelID: request.ChannelID,
		Start: localIfSet(input.Start, s.loc), End: localIfSet(input.End, s.loc), Type: input.Type, Category: input.Category,
		NoteContains: input.NoteContains, MinAmountVND: input.MinAmountVND, MaxAmountVND: input.MaxAmountVND,
	}
	if input.Start.IsZero() != input.End.IsZero() || (!input.Start.IsZero() && !input.Start.Before(input.End)) {
		return sqlite.SearchFilter{}, clarification("Khoảng thời gian tìm kiếm chưa hợp lệ; hãy cho biết mốc bắt đầu và kết thúc.")
	}
	if err := validateFilters(input.Type, input.Category, input.MinAmountVND, input.MaxAmountVND); err != nil {
		return sqlite.SearchFilter{}, err
	}
	if len([]rune(input.NoteContains)) > ledger.MaxTextLength {
		return sqlite.SearchFilter{}, errors.New("search text is too long")
	}
	return filter, nil
}

func validateFilters(transactionType ledger.TransactionType, category ledger.Category, minAmount, maxAmount ledger.AmountVND) error {
	if transactionType != "" {
		if err := ledger.ValidateTransactionType(transactionType); err != nil {
			return err
		}
	}
	if category != "" {
		if transactionType != "" {
			if err := ledger.ValidateCategory(transactionType, category); err != nil {
				return err
			}
		} else {
			valid := false
			for _, candidate := range append(ledger.IncomeCategories(), ledger.ExpenseCategories()...) {
				if candidate == category {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("unsupported category %q", category)
			}
		}
	}
	if minAmount < 0 || maxAmount < 0 || (maxAmount > 0 && minAmount > maxAmount) {
		return errors.New("invalid amount range")
	}
	return nil
}

func localIfSet(value time.Time, location *time.Location) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.In(location)
}

func (s *LedgerService) local(value time.Time) time.Time { return value.In(s.loc) }

// ResolveUnique resolves a target using the authorized scope. Zero or
// multiple matches are clarification cases, never implicit mutations.
func (s *LedgerService) ResolveUnique(ctx context.Context, request ledger.RequestContext, input SearchRequest, operation string) (ledger.Transaction, error) {
	result, err := s.Search(ctx, request, input)
	if err != nil {
		return ledger.Transaction{}, err
	}
	if result.Total == 0 {
		return ledger.Transaction{}, clarification(fmt.Sprintf("Mình không tìm thấy giao dịch để %s. Bạn hãy cho thêm ngày, số tiền hoặc nội dung.", operation))
	}
	if result.Total != 1 {
		return ledger.Transaction{}, clarification(fmt.Sprintf("Có %d giao dịch phù hợp để %s. Bạn hãy nêu rõ giao dịch cần chọn.", result.Total, operation))
	}
	return result.Transactions[0], nil
}

// ConfirmMutations renders an authoritative, concise Vietnamese confirmation.
func ConfirmMutations(transactions []ledger.Transaction) string {
	if len(transactions) == 0 {
		return "Không có giao dịch nào được ghi."
	}
	var builder strings.Builder
	if len(transactions) == 1 {
		builder.WriteString("Đã ghi giao dịch: ")
	} else {
		builder.WriteString("Đã ghi ")
		builder.WriteString(strconv.Itoa(len(transactions)))
		builder.WriteString(" giao dịch:\n")
	}
	for i, transaction := range transactions {
		if i > 0 {
			builder.WriteByte('\n')
		}
		if len(transactions) == 1 {
			builder.WriteString(typeLabel(transaction.Type))
			builder.WriteByte(' ')
		} else {
			builder.WriteString(strconv.Itoa(i + 1))
			builder.WriteString(". ")
		}
		builder.WriteString(formatAmount(transaction.AmountVND))
		builder.WriteString(" VND - ")
		builder.WriteString(categoryLabel(transaction.Category))
		if transaction.Note != "" {
			builder.WriteString(" - ")
			builder.WriteString(transaction.Note)
		}
	}
	return builder.String()
}

func ConfirmMutationBatch(mutations []Mutation, transactions []ledger.Transaction) string {
	if len(mutations) == 1 && len(transactions) == 1 {
		verb := map[MutationKind]string{MutationUpdate: "Đã cập nhật giao dịch", MutationRestore: "Đã khôi phục giao dịch", MutationCreate: "Đã ghi giao dịch"}[mutations[0].Kind]
		if verb != "" {
			transaction := transactions[0]
			text := verb + ": " + typeLabel(transaction.Type) + " " + formatAmount(transaction.AmountVND) + " VND - " + categoryLabel(transaction.Category)
			if transaction.Note != "" {
				text += " - " + transaction.Note
			}
			return text
		}
	}
	return ConfirmMutations(transactions)
}

func categoryLabel(value ledger.Category) string {
	labels := map[ledger.Category]string{
		ledger.CategoryFood: "ăn uống", ledger.CategoryTransport: "đi lại", ledger.CategoryHousing: "nhà ở",
		ledger.CategoryUtilities: "tiện ích", ledger.CategoryShopping: "mua sắm", ledger.CategoryHealth: "sức khỏe",
		ledger.CategoryEducation: "giáo dục", ledger.CategoryEntertainment: "giải trí", ledger.CategoryTravel: "du lịch",
		ledger.CategoryInsurance: "bảo hiểm", ledger.CategoryTaxFee: "thuế phí", ledger.CategoryFamily: "gia đình",
		ledger.CategoryPet: "thú cưng", ledger.CategoryWork: "công việc", ledger.CategoryDebtFinance: "nợ tài chính",
		ledger.CategoryOther: "khác", ledger.CategorySalary: "lương", ledger.CategoryBonus: "thưởng",
		ledger.CategoryFreelance: "freelance", ledger.CategoryBusiness: "kinh doanh", ledger.CategoryInvestmentReturn: "đầu tư",
		ledger.CategoryRefund: "hoàn tiền", ledger.CategoryGift: "quà tặng",
	}
	if label, ok := labels[value]; ok {
		return label
	}
	return string(value)
}

func typeLabel(value ledger.TransactionType) string {
	if value == ledger.TransactionIncome {
		return "thu"
	}
	return "chi"
}

func formatAmount(value ledger.AmountVND) string {
	negative := value < 0
	if negative {
		value = -value
	}
	digits := strconv.FormatInt(int64(value), 10)
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "." + digits[i:]
	}
	if negative {
		return "-" + digits
	}
	return digits
}

// ExportCSV generates stable RFC3339 CSV for authorized, non-deleted rows.
func (s *LedgerService) ExportCSV(ctx context.Context, request ledger.RequestContext, input SearchRequest) ([]byte, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	filter, err := s.searchFilter(request, input)
	if err != nil {
		return nil, err
	}
	var transactions []ledger.Transaction
	if exporter, ok := s.store.(LedgerExporter); ok {
		transactions, err = exporter.Export(ctx, filter)
	} else {
		result, searchErr := s.store.Search(ctx, filter)
		if searchErr != nil {
			return nil, searchErr
		}
		if result.Truncated {
			return nil, errors.New("export result exceeds search limit")
		}
		transactions = result.Transactions
	}
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := writeCSV(&buffer, transactions); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeCSV(writer io.Writer, transactions []ledger.Transaction) error {
	csvWriter := csv.NewWriter(writer)
	if err := csvWriter.Write([]string{"id", "type", "amount_vnd", "category", "note", "occurred_at", "creator_user_id", "guild_id", "channel_id", "source_message_id", "created_at", "updated_at", "deleted_at"}); err != nil {
		return err
	}
	for _, transaction := range transactions {
		if transaction.DeletedAt != nil {
			continue
		}
		if err := csvWriter.Write([]string{
			strconv.FormatInt(transaction.ID, 10), string(transaction.Type), strconv.FormatInt(int64(transaction.AmountVND), 10), string(transaction.Category), transaction.Note,
			transaction.OccurredAt.UTC().Format(time.RFC3339Nano), transaction.CreatorUserID, transaction.GuildID, transaction.ChannelID, transaction.SourceMessageID,
			transaction.CreatedAt.UTC().Format(time.RFC3339Nano), transaction.UpdatedAt.UTC().Format(time.RFC3339Nano), "",
		}); err != nil {
			return err
		}
	}
	csvWriter.Flush()
	return csvWriter.Error()
}
