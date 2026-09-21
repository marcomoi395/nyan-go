package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"nyan-go/internal/ledger"
	"nyan-go/internal/storage/sqlite"
)

type DispatcherProvider interface {
	RespondWithDispatcher(context.Context, ProviderRequest, func(context.Context, FunctionCall) (string, error)) (ProviderResponse, error)
}

type Orchestrator struct {
	provider AIProvider
	ledger   *LedgerService
	clock    Clock
	deletes  *DeleteConfirmer
	export   []byte
	mu       sync.Mutex
	pending  map[string]pendingRequest
}
type pendingRequest struct {
	message string
	expires time.Time
}

type Analysis struct {
	Facts        []string `json:"facts"`
	Observations []string `json:"observations"`
	Limitations  []string `json:"limitations"`
}

func NormalizeAnalysis(text string) (string, error) {
	var analysis Analysis
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&analysis); err != nil {
		return "", fmt.Errorf("analysis must be JSON facts/observations/limitations: %w", err)
	}
	if analysis.Facts == nil || analysis.Observations == nil || analysis.Limitations == nil {
		return "", errors.New("analysis must include facts, observations, and limitations")
	}
	encoded, err := json.Marshal(analysis)
	return string(encoded), err
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
		delete(o.pending, request.UserID)
	} else if ok {
		delete(o.pending, request.UserID)
		return "Yêu cầu trước đã hết hạn. Bạn hãy gửi lại thông tin giao dịch đầy đủ nhé.", nil
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
		result, mutation, isMutation, dispatchErr := o.dispatch(callCtx, request, call, message)
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
			clarificationErr := clarificationForToolError(applyErr)
			var recoverable *ClarificationError
			if errors.As(clarificationErr, &recoverable) {
				o.pending[request.UserID] = pendingRequest{message: message, expires: o.clock.Now().Add(10 * time.Minute)}
				return recoverable.Message, nil
			}
			return "", applyErr
		}
		return ConfirmMutationBatch(mutations, transactions), nil
	}
	if response.NoAction && strings.TrimSpace(response.Text) == "" {
		return "", nil
	}
	if !strings.HasPrefix(strings.TrimSpace(response.Text), "{") && isClarificationText(response.Text) {
		o.pending[request.UserID] = pendingRequest{message: message, expires: o.clock.Now().Add(10 * time.Minute)}
	}
	if strings.TrimSpace(response.Text) == "" && len(response.FunctionCalls) > 0 {
		if len(toolMessages) > 0 {
			return strings.Join(toolMessages, "\n"), nil
		}
		return "Đã xử lý yêu cầu.", nil
	}
	if strings.HasPrefix(strings.TrimSpace(response.Text), "{") {
		return NormalizeAnalysis(response.Text)
	}
	return response.Text, nil
}

func (o *Orchestrator) ConfirmDelete(ctx context.Context, request ledger.RequestContext, token string) ([]ledger.Transaction, error) {
	return o.deletes.Confirm(ctx, request, token)
}

func (o *Orchestrator) TakeExport() []byte {
	data := append([]byte(nil), o.export...)
	o.export = nil
	return data
}

func (o *Orchestrator) dispatch(ctx context.Context, request ledger.RequestContext, call FunctionCall, sourceMessage string) (string, Mutation, bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &raw); err != nil {
		return "", Mutation{}, false, clarificationForToolError(err)
	}
	if err := validateToolFields(call.Name, raw); err != nil {
		return "", Mutation{}, false, clarificationForToolError(err)
	}
	getString := func(key string) string { var v string; _ = json.Unmarshal(raw[key], &v); return v }
	switch call.Name {
	case "create_transaction":
		input, err := transactionInput(raw, request, sourceMessage)
		if err != nil {
			return "", Mutation{}, false, clarificationForToolError(err)
		}
		return "", Mutation{Kind: MutationCreate, Input: input}, true, nil
	case "update_transaction":
		input, err := transactionInput(raw, request, sourceMessage)
		if err != nil {
			return "", Mutation{}, false, clarificationForToolError(err)
		}
		return "", Mutation{Kind: MutationUpdate, ID: int64(numericArgument(raw["transaction_id"])), Input: input}, true, nil
	case "delete_transaction":
		confirmation, err := o.deletes.Prepare(request, []int64{numericArgument(raw["transaction_id"])}, "giao dịch")
		if err != nil {
			return "", Mutation{}, false, err
		}
		return "Xác nhận xóa giao dịch trong 5 phút: delete:" + confirmation.Token, Mutation{}, false, nil
	case "restore_transaction":
		return "", Mutation{Kind: MutationRestore, ID: int64(numericArgument(raw["transaction_id"]))}, true, nil
	case "search_transactions":
		result, err := o.ledger.Search(ctx, request, SearchRequest{Start: parseTime(getString("start")), End: parseTime(getString("end")), Type: ledger.TransactionType(getString("type")), Category: ledger.Category(getString("category")), NoteContains: getString("note"), MinAmountVND: ledger.AmountVND(numericArgument(raw["min_amount_vnd"])), MaxAmountVND: ledger.AmountVND(numericArgument(raw["max_amount_vnd"]))})
		if err != nil {
			return "", Mutation{}, false, err
		}
		encoded, err := marshal(compactSearchResult(result))
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
		o.export = append([]byte(nil), result...)
		return "Đã chuẩn bị tệp CSV giao dịch.", Mutation{}, false, nil
	default:
		return "", Mutation{}, false, fmt.Errorf("unsupported tool %q", call.Name)
	}
}

func numericArgument(raw json.RawMessage) int64 {
	n, _ := parseAmount(raw)
	return n
}

func transactionInput(raw map[string]json.RawMessage, request ledger.RequestContext, sourceMessage string) (ledger.TransactionInput, error) {
	getString := func(key string) string { var value string; _ = json.Unmarshal(raw[key], &value); return value }
	note := getString("note")
	if strings.TrimSpace(note) == "" {
		note = subjectFromMessage(sourceMessage)
	}
	category := normalizeCategory(getString("category"), note)
	transactionType := inferTransactionType(getString("type"), category, note)
	if category == "" && strings.TrimSpace(note) == "" {
		return ledger.TransactionInput{}, clarification("Bạn cho mình biết giao dịch này là gì nhé.")
	}
	occurredAt, err := parseOccurredAt(getString("occurred_at"), request.ReceivedAt)
	if err != nil {
		return ledger.TransactionInput{}, err
	}
	return ledger.TransactionInput{Type: transactionType, AmountVND: ledger.AmountVND(numericArgument(raw["amount_vnd"])), Category: category, Note: note, OccurredAt: occurredAt}, nil
}

type modelTransaction struct {
	ID         int64                  `json:"id"`
	Type       ledger.TransactionType `json:"type"`
	AmountVND  ledger.AmountVND       `json:"amount_vnd"`
	Category   ledger.Category        `json:"category"`
	Note       string                 `json:"note"`
	OccurredAt time.Time              `json:"occurred_at"`
}

type modelSearchResult struct {
	Transactions []modelTransaction `json:"transactions"`
	Total        int                `json:"total"`
	Truncated    bool               `json:"truncated"`
}

func compactSearchResult(result sqlite.SearchResult) modelSearchResult {
	transactions := make([]modelTransaction, 0, len(result.Transactions))
	for _, transaction := range result.Transactions {
		transactions = append(transactions, modelTransaction{ID: transaction.ID, Type: transaction.Type, AmountVND: transaction.AmountVND, Category: transaction.Category, Note: transaction.Note, OccurredAt: transaction.OccurredAt})
	}
	return modelSearchResult{Transactions: transactions, Total: result.Total, Truncated: result.Truncated}
}

func validateToolFields(name string, raw map[string]json.RawMessage) error {
	allowed := map[string]map[string]string{
		"create_transaction": {"type": "string", "amount_vnd": "int!", "category": "string", "note": "string", "occurred_at": "time"},
		"update_transaction": {"transaction_id": "int!", "type": "string!", "amount_vnd": "int!", "category": "string!", "note": "string", "occurred_at": "time"},
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
			if v, ok := parseAmount(value); !ok || v <= 0 {
				return fmt.Errorf("invalid field %q", key)
			}
		case "time":
			var v string
			if err := json.Unmarshal(value, &v); err != nil || required && strings.TrimSpace(v) == "" {
				return fmt.Errorf("invalid field %q", key)
			}
			if strings.TrimSpace(v) != "" {
				if !validToolTime(v) {
					return fmt.Errorf("invalid field %q", key)
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
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed
	}
	location, err := time.LoadLocation(ApplicationTimezone)
	if err != nil {
		return time.Time{}
	}
	parsed, _ := time.ParseInLocation("2006-01-02", value, location)
	return parsed
}

func parseOccurredAt(value string, receivedAt time.Time) (time.Time, error) {
	location, err := time.LoadLocation(ApplicationTimezone)
	if err != nil {
		return time.Time{}, err
	}
	receivedAt = receivedAt.In(location)
	value = strings.TrimSpace(value)
	if value == "" {
		return receivedAt, nil
	}
	if hour, ok := withTimeOfDay(receivedAt, foldVietnamese(value)); ok {
		return hour, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.In(location), nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02", value, location); err == nil {
		return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), receivedAt.Hour(), receivedAt.Minute(), receivedAt.Second(), receivedAt.Nanosecond(), location), nil
	}
	words := strings.Fields(foldVietnamese(value))
	if len(words) >= 2 && (words[0] == "hom" || words[0] == "ngay") {
		days := map[string]int{"nay": 0, "qua": -1, "mai": 1}
		if offset, ok := days[words[1]]; ok {
			date := receivedAt.AddDate(0, 0, offset)
			if len(words) > 2 {
				date, _ = withTimeOfDay(date, words[len(words)-1])
			}
			return date, nil
		}
		if words[0] == "ngay" {
			if parsed, parseErr := time.ParseInLocation("02/01", words[1], location); parseErr == nil {
				return time.Date(receivedAt.Year(), parsed.Month(), parsed.Day(), receivedAt.Hour(), receivedAt.Minute(), receivedAt.Second(), receivedAt.Nanosecond(), location), nil
			}
		}
	}
	if parsed, err := time.ParseInLocation("02/01", value, location); err == nil {
		return time.Date(receivedAt.Year(), parsed.Month(), parsed.Day(), receivedAt.Hour(), receivedAt.Minute(), receivedAt.Second(), receivedAt.Nanosecond(), location), nil
	}
	return time.Time{}, fmt.Errorf("invalid occurred_at %q", value)
}

func withTimeOfDay(value time.Time, part string) (time.Time, bool) {
	hour, ok := map[string]int{"sang": 8, "trua": 12, "chieu": 15, "toi": 20}[part]
	if !ok {
		return value, false
	}
	return time.Date(value.Year(), value.Month(), value.Day(), hour, 0, 0, 0, value.Location()), true
}

func parseTimeValue(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return parsed, nil
	}
	return time.Parse("2006-01-02", value)
}

func validToolTime(value string) bool {
	if _, err := parseTimeValue(value); err == nil {
		return true
	}
	value = foldVietnamese(value)
	if _, err := time.ParseInLocation("02/01", value, time.Local); err == nil {
		return true
	}
	words := strings.Fields(value)
	if len(words) >= 2 && (words[0] == "hom" || words[0] == "ngay") {
		return true
	}
	for _, part := range []string{"sang", "trua", "chieu", "toi"} {
		if value == part {
			return true
		}
	}
	return false
}

func parseAmount(raw json.RawMessage) (int64, bool) {
	var value int64
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, false
	}
	text = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(text, "đ"), "d")))
	text = strings.ReplaceAll(text, " ", "")
	if strings.HasSuffix(text, "k") || strings.HasSuffix(text, "nghin") {
		text = strings.TrimSuffix(strings.TrimSuffix(text, "k"), "nghin")
		return parseInteger(text, 1000)
	}
	for _, suffix := range []string{"trieu", "tr"} {
		if index := strings.Index(text, suffix); index > 0 {
			wholeText, fractionText := text[:index], strings.TrimPrefix(text[index+len(suffix):], ".")
			if fractionText != "" {
				whole, wholeOK := parseDigits(wholeText)
				fraction, fractionOK := parseDigits(fractionText)
				if wholeOK && fractionOK && len(fractionText) <= 3 {
					return whole*1_000_000 + fraction*int64Pow10(6-len(fractionText)), true
				}
			}
			if parts := strings.FieldsFunc(wholeText, func(r rune) bool { return r == '.' || r == ',' }); len(parts) == 2 && len(parts[1]) <= 3 {
				whole, wholeOK := parseDigits(parts[0])
				fraction, fractionOK := parseDigits(parts[1])
				if wholeOK && fractionOK {
					return whole*1_000_000 + fraction*int64Pow10(6-len(parts[1])), true
				}
			}
			if whole, ok := parseDigits(wholeText); ok {
				return whole * 1_000_000, true
			}
		}
	}
	return parseDigits(text)
}

func parseInteger(value string, multiplier int64) (int64, bool) {
	number, ok := parseDigits(value)
	return number * multiplier, ok
}

func parseDigits(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '.' || r == ',' })
	for i, part := range parts {
		if part == "" || !allDigits(part) || i > 0 && len(part) != 3 || i == 0 && len(part) > 3 {
			return 0, false
		}
	}
	number, err := strconv.ParseInt(strings.Join(parts, ""), 10, 64)
	if err != nil {
		return 0, false
	}
	return number, true
}

func allDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func int64Pow10(power int) int64 {
	value := int64(1)
	for i := 0; i < power; i++ {
		value *= 10
	}
	return value
}

func clarificationForToolError(err error) error {
	message := err.Error()
	switch {
	case strings.Contains(message, "amount"):
		return clarification("Bạn cho mình số tiền chính xác nhé.")
	case strings.Contains(message, "category"):
		return clarification("Bạn muốn xếp giao dịch này vào danh mục nào?")
	case strings.Contains(message, "type"):
		return clarification("Khoản này là thu hay chi?")
	case strings.Contains(message, "occurred_at"), strings.Contains(message, "time"):
		return clarification("Bạn cho mình biết ngày giao dịch nhé.")
	default:
		return err
	}
}

func normalizeCategory(value, note string) ledger.Category {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(note))
	}
	value = foldVietnamese(value)
	mappings := map[string]ledger.Category{
		"food": ledger.CategoryFood, "an uong": ledger.CategoryFood, "pho": ledger.CategoryFood, "bun": ledger.CategoryFood, "com": ledger.CategoryFood, "ca phe": ledger.CategoryFood, "bia": ledger.CategoryFood,
		"transport": ledger.CategoryTransport, "di lai": ledger.CategoryTransport, "xang": ledger.CategoryTransport, "taxi": ledger.CategoryTransport, "grab": ledger.CategoryTransport,
		"health": ledger.CategoryHealth, "suc khoe": ledger.CategoryHealth, "thuoc": ledger.CategoryHealth, "kham benh": ledger.CategoryHealth,
		"utilities": ledger.CategoryUtilities, "tien ich": ledger.CategoryUtilities, "dien": ledger.CategoryUtilities, "nuoc": ledger.CategoryUtilities, "internet": ledger.CategoryUtilities,
		"salary": ledger.CategorySalary, "luong": ledger.CategorySalary, "thu nhap": ledger.CategorySalary, "nhan tien": ledger.CategorySalary, "bonus": ledger.CategoryBonus, "thuong": ledger.CategoryBonus,
		"other": ledger.CategoryOther, "khac": ledger.CategoryOther, "khong phan loai": ledger.CategoryOther,
	}
	if category, ok := mappings[value]; ok {
		return category
	}
	for phrase, category := range mappings {
		if strings.Contains(value, phrase) {
			return category
		}
	}
	return ledger.Category(value)
}

func isClarificationText(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.HasSuffix(value, "?") || strings.Contains(value, "hãy cho biết") || strings.Contains(value, "cho mình biết")
}

func foldVietnamese(value string) string {
	return strings.NewReplacer("ă", "a", "â", "a", "đ", "d", "ê", "e", "ô", "o", "ơ", "o", "ư", "u", "á", "a", "à", "a", "ả", "a", "ã", "a", "ạ", "a", "é", "e", "è", "e", "ẻ", "e", "ẽ", "e", "ẹ", "e", "í", "i", "ì", "i", "ỉ", "i", "ĩ", "i", "ị", "i", "ó", "o", "ò", "o", "ỏ", "o", "õ", "o", "ọ", "o", "ú", "u", "ù", "u", "ủ", "u", "ũ", "u", "ụ", "u").Replace(strings.ToLower(value))
}

func inferTransactionType(value string, category ledger.Category, note string) ledger.TransactionType {
	if strings.TrimSpace(value) != "" {
		return ledger.TransactionType(value)
	}
	if category == ledger.CategorySalary || category == ledger.CategoryBonus || category == ledger.CategoryFreelance || category == ledger.CategoryBusiness || category == ledger.CategoryInvestmentReturn || category == ledger.CategoryRefund || category == ledger.CategoryGift {
		return ledger.TransactionIncome
	}
	note = foldVietnamese(note)
	for _, word := range []string{"luong", "thu nhap", "nhan tien", "tien ve", "thuong"} {
		if strings.Contains(note, word) {
			return ledger.TransactionIncome
		}
	}
	for _, word := range []string{"mua", "an", "uống", "tra tien", "chi", "xang", "taxi", "thuoc"} {
		if strings.Contains(note, foldVietnamese(word)) {
			return ledger.TransactionExpense
		}
	}
	if category != "" {
		return ledger.TransactionExpense
	}
	return ""
}

func subjectFromMessage(message string) string {
	words := strings.Fields(strings.TrimSpace(message))
	kept := make([]string, 0, len(words))
	for _, word := range words {
		value := foldVietnamese(strings.ToLower(strings.Trim(word, ",.!?")))
		if value == "hom" || value == "nay" || value == "qua" || value == "mai" || value == "sang" || value == "trua" || value == "chieu" || value == "toi" || value == "expense" || value == "income" || value == "chi" || value == "thu" {
			continue
		}
		if _, ok := parseAmount(json.RawMessage(`"` + value + `"`)); ok {
			continue
		}
		kept = append(kept, word)
	}
	return strings.Join(kept, " ")
}

func marshal(value any) (string, error) {
	encoded, err := json.Marshal(value)
	return string(encoded), err
}
