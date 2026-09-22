package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	appsvc "nyan-go/internal/app"
	"nyan-go/internal/ledger"
	"nyan-go/internal/storage/sqlite"
)

const (
	csrfCookieName = "nyan_csrf"
	pageSize       = 25
)

//go:embed templates.html static/app.css
var assets embed.FS

type Config struct {
	Service   *appsvc.LedgerService
	UserID    string
	GuildID   string
	ChannelID string
	Location  *time.Location
	Now       func() time.Time
}

type Server struct {
	service   *appsvc.LedgerService
	userID    string
	guildID   string
	channelID string
	location  *time.Location
	now       func() time.Time
	templates *template.Template
	handler   http.Handler
}

type pageData struct {
	Title             string
	Active            string
	CSRF              string
	Error             string
	Success           string
	Transactions      []ledger.Transaction
	Transaction       ledger.Transaction
	Filters           filterView
	Form              formView
	Report            reportView
	Page              int
	First             int
	Last              int
	Total             int
	PreviousURL       string
	NextURL           string
	IncomeCategories  []ledger.Category
	ExpenseCategories []ledger.Category
}

type filterView struct {
	Start, End, Type, Category, Note, MinAmount, MaxAmount string
}

type formView struct {
	Type, Amount, Category, Note, OccurredAt string
}

type reportView struct {
	Start, End       string
	Statistics       sqlite.Statistics
	Categories       []reportGroup
	Days             []reportGroup
	CategoryMaxValue ledger.AmountVND
}

type reportGroup struct {
	Key, Label                     string
	TransactionCount               int
	TotalIncome, TotalExpense, Net ledger.AmountVND
	Value                          ledger.AmountVND
}

func New(config Config) (*Server, error) {
	if config.Service == nil {
		return nil, errors.New("ledger service is required")
	}
	if strings.TrimSpace(config.UserID) == "" || strings.TrimSpace(config.GuildID) == "" || strings.TrimSpace(config.ChannelID) == "" {
		return nil, errors.New("web identity fields are required")
	}
	if config.Location == nil {
		return nil, errors.New("web location is required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	funcs := template.FuncMap{
		"money":         formatAmount,
		"categoryLabel": categoryLabel,
		"typeLabel":     typeLabel,
		"localDateTime": func(value time.Time) string { return value.In(config.Location).Format("02/01/2006 15:04") },
		"percent":       func(value float64) string { return fmt.Sprintf("%+.1f%%", value) },
	}
	templates, err := template.New("pages").Funcs(funcs).ParseFS(assets, "templates.html")
	if err != nil {
		return nil, fmt.Errorf("parse web templates: %w", err)
	}
	server := &Server{
		service: config.Service, userID: config.UserID, guildID: config.GuildID, channelID: config.ChannelID,
		location: config.Location, now: config.Now, templates: templates,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", server.dashboard)
	mux.HandleFunc("GET /transactions", server.transactions)
	mux.HandleFunc("GET /transactions/new", server.newTransaction)
	mux.HandleFunc("POST /transactions", server.createTransaction)
	mux.HandleFunc("GET /transactions/{id}/delete", server.confirmDelete)
	mux.HandleFunc("POST /transactions/{id}/delete", server.deleteTransaction)
	mux.HandleFunc("GET /reports", server.reports)
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, fmt.Errorf("open web assets: %w", err)
	}
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(staticFS))))
	server.handler = securityHeaders(mux)
	return server, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	start, end := monthRange(s.now().In(s.location))
	report, err := s.buildReport(r.Context(), start, end)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	recent, err := s.service.Search(r.Context(), s.requestContext(), appsvc.SearchRequest{Limit: 6})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	data := pageData{Title: "Tổng quan", Active: "dashboard", Report: report, Transactions: recent.Transactions}
	s.render(w, r, http.StatusOK, "dashboard", data)
}

func (s *Server) transactions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page := positiveInt(query.Get("page"), 1)
	filters := filterView{
		Start: query.Get("start"), End: query.Get("end"), Type: query.Get("type"), Category: query.Get("category"),
		Note: query.Get("note"), MinAmount: query.Get("min_amount"), MaxAmount: query.Get("max_amount"),
	}
	request, err := s.searchRequest(filters, page)
	if err != nil {
		data := pageData{Title: "Giao dịch", Active: "transactions", Filters: filters, Page: page, Error: err.Error(), IncomeCategories: ledger.IncomeCategories(), ExpenseCategories: ledger.ExpenseCategories()}
		s.render(w, r, http.StatusUnprocessableEntity, "transactions", data)
		return
	}
	result, err := s.service.Search(r.Context(), s.requestContext(), request)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if len(result.Transactions) == 0 && result.Total > 0 && page > 1 {
		lastPage := (result.Total + pageSize - 1) / pageSize
		http.Redirect(w, r, pageURL(query, lastPage), http.StatusSeeOther)
		return
	}
	first := 0
	if result.Total > 0 {
		first = request.Offset + 1
	}
	last := request.Offset + len(result.Transactions)
	data := pageData{
		Title: "Giao dịch", Active: "transactions", Filters: filters, Transactions: result.Transactions,
		Page: page, First: first, Last: last, Total: result.Total,
		IncomeCategories: ledger.IncomeCategories(), ExpenseCategories: ledger.ExpenseCategories(),
	}
	if page > 1 {
		data.PreviousURL = pageURL(query, page-1)
	}
	if result.Truncated {
		data.NextURL = pageURL(query, page+1)
	}
	if query.Get("created") == "1" {
		data.Success = "Đã thêm giao dịch."
	}
	if query.Get("deleted") == "1" {
		data.Success = "Đã xóa giao dịch."
	}
	s.render(w, r, http.StatusOK, "transactions", data)
}

func (s *Server) newTransaction(w http.ResponseWriter, r *http.Request) {
	form := formView{Type: string(ledger.TransactionExpense), OccurredAt: s.now().In(s.location).Format("2006-01-02T15:04")}
	s.renderForm(w, r, http.StatusOK, form, "")
}

func (s *Server) createTransaction(w http.ResponseWriter, r *http.Request) {
	if !s.validPost(w, r) {
		return
	}
	form := formView{
		Type: r.Form.Get("type"), Amount: r.Form.Get("amount"), Category: r.Form.Get("category"),
		Note: r.Form.Get("note"), OccurredAt: r.Form.Get("occurred_at"),
	}
	amount, err := parseAmount(form.Amount, true)
	if err != nil {
		s.renderForm(w, r, http.StatusUnprocessableEntity, form, err.Error())
		return
	}
	occurredAt, err := time.ParseInLocation("2006-01-02T15:04", form.OccurredAt, s.location)
	if err != nil {
		s.renderForm(w, r, http.StatusUnprocessableEntity, form, "Ngày giờ giao dịch chưa hợp lệ.")
		return
	}
	input := ledger.TransactionInput{
		Type: ledger.TransactionType(form.Type), AmountVND: amount, Category: ledger.Category(form.Category),
		Note: strings.TrimSpace(form.Note), OccurredAt: occurredAt,
	}
	if err := input.Validate(); err != nil {
		s.renderForm(w, r, http.StatusUnprocessableEntity, form, validationMessage(err))
		return
	}
	request, err := s.mutationContext()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if _, err := s.service.CreateBatch(r.Context(), request, []ledger.TransactionInput{input}); err != nil {
		s.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, "/transactions?created=1", http.StatusSeeOther)
}

func (s *Server) confirmDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	transaction, err := s.service.GetTransaction(r.Context(), s.requestContext(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data := pageData{Title: "Xác nhận xóa", Active: "transactions", Transaction: transaction}
	s.render(w, r, http.StatusOK, "delete", data)
}

func (s *Server) deleteTransaction(w http.ResponseWriter, r *http.Request) {
	if !s.validPost(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	request, err := s.mutationContext()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if _, err := s.service.DeleteTransactions(r.Context(), request, []int64{id}); err != nil {
		s.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, "/transactions?deleted=1", http.StatusSeeOther)
}

func (s *Server) reports(w http.ResponseWriter, r *http.Request) {
	now := s.now().In(s.location)
	defaultStart, defaultEnd := monthRange(now)
	startText, endText := r.URL.Query().Get("start"), r.URL.Query().Get("end")
	start, end := defaultStart, defaultEnd
	if startText != "" || endText != "" {
		var err error
		start, end, err = parseDateRange(startText, endText, s.location)
		if err != nil {
			data := pageData{Title: "Báo cáo", Active: "reports", Error: err.Error(), Report: reportView{Start: startText, End: endText}}
			s.render(w, r, http.StatusUnprocessableEntity, "reports", data)
			return
		}
	}
	report, err := s.buildReport(r.Context(), start, end)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	data := pageData{Title: "Báo cáo", Active: "reports", Report: report}
	s.render(w, r, http.StatusOK, "reports", data)
}

func (s *Server) buildReport(ctx context.Context, start, end time.Time) (reportView, error) {
	compareStart, compareEnd := comparisonRange(start, end)
	request := appsvc.StatisticsRequest{
		Start: start, End: end, Group: "category", CompareStart: compareStart, CompareEnd: compareEnd,
	}
	statistics, err := s.service.Statistics(ctx, s.requestContext(), request)
	if err != nil {
		return reportView{}, err
	}
	daily, err := s.service.Statistics(ctx, s.requestContext(), appsvc.StatisticsRequest{Start: start, End: end, Group: "day"})
	if err != nil {
		return reportView{}, err
	}
	report := reportView{Start: start.Format("2006-01-02"), End: end.AddDate(0, 0, -1).Format("2006-01-02"), Statistics: statistics}
	for _, group := range statistics.Groups {
		value := group.TotalIncome
		if group.TotalExpense > value {
			value = group.TotalExpense
		}
		if value > report.CategoryMaxValue {
			report.CategoryMaxValue = value
		}
		report.Categories = append(report.Categories, reportGroup{
			Key: group.Key, Label: categoryLabel(ledger.Category(group.Key)), TransactionCount: group.TransactionCount,
			TotalIncome: group.TotalIncome, TotalExpense: group.TotalExpense, Net: group.Net, Value: value,
		})
	}
	if report.CategoryMaxValue == 0 {
		report.CategoryMaxValue = 1
	}
	for _, group := range daily.Groups {
		label := group.Key
		if day, err := time.Parse("2006-01-02", group.Key); err == nil {
			label = day.Format("02/01")
		}
		report.Days = append(report.Days, reportGroup{
			Key: group.Key, Label: label, TransactionCount: group.TransactionCount,
			TotalIncome: group.TotalIncome, TotalExpense: group.TotalExpense, Net: group.Net,
		})
	}
	return report, nil
}

func (s *Server) renderForm(w http.ResponseWriter, r *http.Request, status int, form formView, message string) {
	data := pageData{
		Title: "Thêm giao dịch", Active: "new", Form: form, Error: message,
		IncomeCategories: ledger.IncomeCategories(), ExpenseCategories: ledger.ExpenseCategories(),
	}
	s.render(w, r, status, "new", data)
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, data pageData) {
	token, err := csrfToken(w, r)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	data.CSRF = token
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("action=web_render page=%s error=%v", name, err)
	}
}

func (s *Server) internalError(w http.ResponseWriter, _ *http.Request, err error) {
	log.Printf("action=web_request success=false error=%v", err)
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "Không thể xử lý yêu cầu lúc này.", http.StatusInternalServerError)
}

func (s *Server) validPost(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
		http.Error(w, "Yêu cầu không hợp lệ.", http.StatusForbidden)
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
			http.Error(w, "Yêu cầu không hợp lệ.", http.StatusForbidden)
			return false
		}
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Biểu mẫu không hợp lệ.", http.StatusBadRequest)
		return false
	}
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil || len(cookie.Value) != 64 || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.Form.Get("csrf_token"))) != 1 {
		http.Error(w, "Yêu cầu không hợp lệ.", http.StatusForbidden)
		return false
	}
	return true
}

func csrfToken(w http.ResponseWriter, r *http.Request) (string, error) {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && len(cookie.Value) == 64 {
		return cookie.Value, nil
	}
	value, err := randomHex(32)
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: value, Path: "/", MaxAge: 86400, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	return value, nil
}

func (s *Server) requestContext() ledger.RequestContext {
	return ledger.RequestContext{
		UserID: s.userID, GuildID: s.guildID, ChannelID: s.channelID,
		SourceMessageID: "web:read", ReceivedAt: s.now().In(s.location),
	}
}

func (s *Server) mutationContext() (ledger.RequestContext, error) {
	id, err := randomHex(16)
	if err != nil {
		return ledger.RequestContext{}, err
	}
	request := s.requestContext()
	request.SourceMessageID = "web:" + id
	return request, nil
}

func (s *Server) searchRequest(filters filterView, page int) (appsvc.SearchRequest, error) {
	start, end, err := parseOptionalDateRange(filters.Start, filters.End, s.location)
	if err != nil {
		return appsvc.SearchRequest{}, err
	}
	minAmount, err := parseAmount(filters.MinAmount, false)
	if err != nil {
		return appsvc.SearchRequest{}, errors.New("Số tiền tối thiểu chưa hợp lệ.")
	}
	maxAmount, err := parseAmount(filters.MaxAmount, false)
	if err != nil {
		return appsvc.SearchRequest{}, errors.New("Số tiền tối đa chưa hợp lệ.")
	}
	return appsvc.SearchRequest{
		Start: start, End: end, Type: ledger.TransactionType(filters.Type), Category: ledger.Category(filters.Category),
		NoteContains: strings.TrimSpace(filters.Note), MinAmountVND: minAmount, MaxAmountVND: maxAmount,
		Limit: pageSize, Offset: (page - 1) * pageSize,
	}, nil
}

func parseOptionalDateRange(startText, endText string, location *time.Location) (time.Time, time.Time, error) {
	if startText == "" && endText == "" {
		return time.Time{}, time.Time{}, nil
	}
	return parseDateRange(startText, endText, location)
}

func parseDateRange(startText, endText string, location *time.Location) (time.Time, time.Time, error) {
	if startText == "" || endText == "" {
		return time.Time{}, time.Time{}, errors.New("Hãy chọn đủ ngày bắt đầu và ngày kết thúc.")
	}
	start, err := time.ParseInLocation("2006-01-02", startText, location)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("Ngày bắt đầu chưa hợp lệ.")
	}
	endInclusive, err := time.ParseInLocation("2006-01-02", endText, location)
	if err != nil || endInclusive.Before(start) {
		return time.Time{}, time.Time{}, errors.New("Ngày kết thúc chưa hợp lệ.")
	}
	return start, endInclusive.AddDate(0, 0, 1), nil
}

func parseAmount(raw string, required bool) (ledger.AmountVND, error) {
	cleaned := strings.NewReplacer(".", "", ",", "", " ", "").Replace(strings.TrimSpace(raw))
	if cleaned == "" && !required {
		return 0, nil
	}
	value, err := strconv.ParseInt(cleaned, 10, 64)
	if err != nil || value <= 0 {
		return 0, errors.New("Số tiền phải là số nguyên VND lớn hơn 0.")
	}
	return ledger.AmountVND(value), nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid transaction ID")
	}
	return id, nil
}

func randomHex(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func monthRange(now time.Time) (time.Time, time.Time) {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	return start, start.AddDate(0, 1, 0)
}

func comparisonRange(start, end time.Time) (time.Time, time.Time) {
	if start.Day() == 1 && end.Equal(start.AddDate(0, 1, 0)) {
		return start.AddDate(0, -1, 0), start
	}
	return start.Add(-end.Sub(start)), start
}

func positiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 1_000_000 {
		return fallback
	}
	return value
}

func pageURL(values url.Values, page int) string {
	copy := url.Values{}
	for key, items := range values {
		copy[key] = append([]string(nil), items...)
	}
	copy.Set("page", strconv.Itoa(page))
	return "/transactions?" + copy.Encode()
}

func validationMessage(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "amount"):
		return "Số tiền phải là số nguyên VND lớn hơn 0."
	case strings.Contains(message, "transaction type"):
		return "Loại giao dịch chưa hợp lệ."
	case strings.Contains(message, "category"):
		return "Danh mục không phù hợp với loại giao dịch."
	case strings.Contains(message, "note exceeds"):
		return "Ghi chú không được vượt quá 2.000 ký tự."
	default:
		return "Thông tin giao dịch chưa hợp lệ."
	}
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

func typeLabel(value ledger.TransactionType) string {
	if value == ledger.TransactionIncome {
		return "Thu"
	}
	return "Chi"
}

func categoryLabel(value ledger.Category) string {
	labels := map[ledger.Category]string{
		ledger.CategoryFood: "Ăn uống", ledger.CategoryTransport: "Đi lại", ledger.CategoryHousing: "Nhà ở",
		ledger.CategoryUtilities: "Tiện ích", ledger.CategoryShopping: "Mua sắm", ledger.CategoryHealth: "Sức khỏe",
		ledger.CategoryEducation: "Giáo dục", ledger.CategoryEntertainment: "Giải trí", ledger.CategoryTravel: "Du lịch",
		ledger.CategoryInsurance: "Bảo hiểm", ledger.CategoryTaxFee: "Thuế phí", ledger.CategoryFamily: "Gia đình",
		ledger.CategoryPet: "Thú cưng", ledger.CategoryWork: "Công việc", ledger.CategoryDebtFinance: "Nợ tài chính",
		ledger.CategoryOther: "Khác", ledger.CategorySalary: "Lương", ledger.CategoryBonus: "Thưởng",
		ledger.CategoryFreelance: "Freelance", ledger.CategoryBusiness: "Kinh doanh", ledger.CategoryInvestmentReturn: "Đầu tư",
		ledger.CategoryRefund: "Hoàn tiền", ledger.CategoryGift: "Quà tặng",
	}
	if label, ok := labels[value]; ok {
		return label
	}
	return string(value)
}
