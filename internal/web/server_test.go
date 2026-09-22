package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	appsvc "nyan-go/internal/app"
	"nyan-go/internal/ledger"
	"nyan-go/internal/storage/sqlite"
)

type webFixture struct {
	server *Server
	store  *sqlite.Store
	now    time.Time
}

func newWebFixture(t *testing.T) webFixture {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	location, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	service, err := appsvc.NewLedgerService(store, location)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 22, 10, 30, 0, 0, location)
	server, err := New(Config{
		Service: service, UserID: "user", GuildID: "guild", ChannelID: "channel", Location: location,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return webFixture{server: server, store: store, now: now}
}

func TestReadPagesRenderWithoutJavaScript(t *testing.T) {
	fixture := newWebFixture(t)
	for _, path := range []string{"/", "/transactions", "/transactions/new", "/reports", "/assets/app.css"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		fixture.server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d body %s", path, response.Code, response.Body.String())
		}
		if response.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("GET %s: missing security headers", path)
		}
		if path != "/assets/app.css" && strings.Contains(strings.ToLower(response.Body.String()), "<script") {
			t.Fatalf("GET %s: unexpected script", path)
		}
		if path != "/assets/app.css" && !strings.Contains(response.Body.String(), "</html>") {
			t.Fatalf("GET %s: partial template render", path)
		}
	}
}

func TestCreateTransactionUsesTrustedScopeAndWebSource(t *testing.T) {
	fixture := newWebFixture(t)
	cookie := csrfCookie(t, fixture.server, "/transactions/new")
	form := url.Values{
		"csrf_token": {cookie.Value}, "type": {"expense"}, "amount": {"45.000"},
		"category": {"food"}, "note": {"phở trưa"}, "occurred_at": {"2026-09-22T12:00"},
	}
	response := postForm(fixture.server, "/transactions", form, cookie, "same-origin")
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/transactions?created=1" {
		t.Fatalf("create: status %d location %q body %s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	result, err := fixture.store.Search(context.Background(), sqlite.SearchFilter{UserID: "user", GuildID: "guild", ChannelID: "channel"})
	if err != nil || result.Total != 1 {
		t.Fatalf("search created transaction: %v %#v", err, result)
	}
	transaction := result.Transactions[0]
	if transaction.AmountVND != 45_000 || transaction.Note != "phở trưa" || !strings.HasPrefix(transaction.SourceMessageID, "web:") {
		t.Fatalf("unexpected transaction: %#v", transaction)
	}
	otherScope, err := fixture.store.Search(context.Background(), sqlite.SearchFilter{UserID: "other", GuildID: "guild", ChannelID: "channel"})
	if err != nil || otherScope.Total != 0 {
		t.Fatalf("transaction escaped trusted scope: %v %#v", err, otherScope)
	}
}

func TestPostRejectsMissingOrCrossSiteCSRF(t *testing.T) {
	fixture := newWebFixture(t)
	form := url.Values{"type": {"expense"}, "amount": {"1"}, "category": {"food"}, "occurred_at": {"2026-09-22T12:00"}}
	missing := postForm(fixture.server, "/transactions", form, nil, "same-origin")
	if missing.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF: status %d", missing.Code)
	}
	cookie := csrfCookie(t, fixture.server, "/transactions/new")
	form.Set("csrf_token", cookie.Value)
	crossSite := postForm(fixture.server, "/transactions", form, cookie, "cross-site")
	if crossSite.Code != http.StatusForbidden {
		t.Fatalf("cross-site CSRF: status %d", crossSite.Code)
	}
}

func TestDeleteRequiresConfirmationPostAndSoftDeletes(t *testing.T) {
	fixture := newWebFixture(t)
	request := ledger.RequestContext{UserID: "user", GuildID: "guild", ChannelID: "channel", SourceMessageID: "seed", ReceivedAt: fixture.now}
	created, err := fixture.store.CreateBatch(context.Background(), request, []ledger.TransactionInput{{
		Type: ledger.TransactionExpense, AmountVND: 30_000, Category: ledger.CategoryTransport, Note: "xe", OccurredAt: fixture.now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	path := "/transactions/" + stringInt(created[0].ID) + "/delete"
	getRequest := httptest.NewRequest(http.MethodGet, path, nil)
	getResponse := httptest.NewRecorder()
	fixture.server.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), "Xóa giao dịch?") {
		t.Fatalf("confirmation: status %d body %s", getResponse.Code, getResponse.Body.String())
	}
	cookies := getResponse.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("confirmation did not issue CSRF cookie")
	}
	form := url.Values{"csrf_token": {cookies[0].Value}}
	postResponse := postForm(fixture.server, path, form, cookies[0], "same-origin")
	if postResponse.Code != http.StatusSeeOther {
		t.Fatalf("delete: status %d body %s", postResponse.Code, postResponse.Body.String())
	}
	result, err := fixture.store.Search(context.Background(), sqlite.SearchFilter{UserID: "user", GuildID: "guild", ChannelID: "channel"})
	if err != nil || result.Total != 0 {
		t.Fatalf("soft-delete search: %v %#v", err, result)
	}
	var deletedAt string
	if err := fixture.store.DB().QueryRow(`SELECT deleted_at FROM transactions WHERE id = ?`, created[0].ID).Scan(&deletedAt); err != nil || deletedAt == "" {
		t.Fatalf("deleted row missing: %v %q", err, deletedAt)
	}
}

func TestComparisonRangeUsesPreviousCalendarMonth(t *testing.T) {
	location, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, location)
	end := start.AddDate(0, 1, 0)
	compareStart, compareEnd := comparisonRange(start, end)
	if want := time.Date(2026, 8, 1, 0, 0, 0, 0, location); !compareStart.Equal(want) || !compareEnd.Equal(start) {
		t.Fatalf("comparison range = %s..%s", compareStart, compareEnd)
	}
}

func TestTransactionsPaginatesAndRedirectsPastLastPage(t *testing.T) {
	fixture := newWebFixture(t)
	for index := 0; index < 26; index++ {
		request := ledger.RequestContext{
			UserID: "user", GuildID: "guild", ChannelID: "channel", SourceMessageID: "page:" + strconv.Itoa(index), ReceivedAt: fixture.now,
		}
		_, err := fixture.store.CreateBatch(context.Background(), request, []ledger.TransactionInput{{
			Type: ledger.TransactionExpense, AmountVND: ledger.AmountVND(index + 1), Category: ledger.CategoryFood, OccurredAt: fixture.now.Add(-time.Duration(index) * time.Hour),
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	firstRequest := httptest.NewRequest(http.MethodGet, "/transactions", nil)
	firstResponse := httptest.NewRecorder()
	fixture.server.ServeHTTP(firstResponse, firstRequest)
	if firstResponse.Code != http.StatusOK || !strings.Contains(firstResponse.Body.String(), "Trang sau") {
		t.Fatalf("first page: status %d body %s", firstResponse.Code, firstResponse.Body.String())
	}
	pastRequest := httptest.NewRequest(http.MethodGet, "/transactions?page=999", nil)
	pastResponse := httptest.NewRecorder()
	fixture.server.ServeHTTP(pastResponse, pastRequest)
	if pastResponse.Code != http.StatusSeeOther || pastResponse.Header().Get("Location") != "/transactions?page=2" {
		t.Fatalf("past last page: status %d location %q", pastResponse.Code, pastResponse.Header().Get("Location"))
	}
}

func csrfCookie(t *testing.T, handler http.Handler, path string) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == csrfCookieName {
			return cookie
		}
	}
	t.Fatal("CSRF cookie missing")
	return nil
}

func postForm(handler http.Handler, path string, form url.Values, cookie *http.Cookie, fetchSite string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "http://example.com")
	request.Header.Set("Sec-Fetch-Site", fetchSite)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func stringInt(value int64) string { return strconv.FormatInt(value, 10) }
