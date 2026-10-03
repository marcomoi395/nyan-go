package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestLoginValidatesPasswordWithoutCallingWebhook(t *testing.T) {
	calls := 0
	handler := newHandler("mật khẩu", "https://discord.com/api/webhooks/123/secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, nil
	})})
	for _, tc := range []struct {
		password string
		want     int
	}{{"", http.StatusUnauthorized}, {"wrong", http.StatusUnauthorized}, {"mật khẩu", http.StatusOK}} {
		request := httptest.NewRequest(http.MethodPost, "/auth", nil)
		request.Header.Set("X-Webhook-Password", base64.StdEncoding.EncodeToString([]byte(tc.password)))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.want || calls != 0 {
			t.Fatalf("login status=%d webhook calls=%d, want %d and no send", response.Code, calls, tc.want)
		}
		if tc.want == http.StatusOK && !strings.Contains(response.Body.String(), `"success":true`) {
			t.Fatalf("login success missing from response: %s", response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/send", nil))
	if response.Code != http.StatusUnauthorized || calls != 0 {
		t.Fatalf("send after login without credential status=%d webhook calls=%d", response.Code, calls)
	}
}

func TestWebhookSendAuthenticatesAndForwardsMultipart(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	handler := newHandler("secret", "https://discord.com/api/webhooks/123/secret", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotBody, _ = io.ReadAll(r.Body)
		gotContentType = r.Header.Get("Content-Type")
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"1","channel_id":"2","content":"hello"}`))}, nil
	})})

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/send", strings.NewReader("large upload")))
	if unauthorized.Code != http.StatusUnauthorized || gotBody != nil {
		t.Fatalf("unauthorized request status=%d reached webhook=%t", unauthorized.Code, gotBody != nil)
	}

	var payload bytes.Buffer
	form := multipart.NewWriter(&payload)
	_ = form.WriteField("content", "hello")
	file, _ := form.CreateFormFile("file", "../../ cat.txt")
	_, _ = file.Write([]byte("nyan"))
	_ = form.Close()
	request := httptest.NewRequest(http.MethodPost, "/send", &payload)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-Webhook-Password", base64.StdEncoding.EncodeToString([]byte("secret")))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"success":true`) {
		t.Fatalf("authorized send status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data;") || !bytes.Contains(gotBody, []byte(`"allowed_mentions":{"parse":[]}`)) || !bytes.Contains(gotBody, []byte("nyan")) || !bytes.Contains(gotBody, []byte(`filename="cat.txt"`)) {
		t.Fatalf("webhook request missing multipart file or mention restrictions: %q", gotContentType)
	}
}

func TestWebhookSendAcceptsUnicodeLimitWithoutFile(t *testing.T) {
	calls := 0
	handler := newHandler("secret", "https://discord.com/api/webhooks/123/secret", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("wait") != "true" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request URL/type = %s / %s", r.URL, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte(strings.Repeat("💸", maxMessageRunes))) || !bytes.Contains(body, []byte(`"allowed_mentions":{"parse":[]}`)) {
			t.Error("webhook payload missing the full Unicode message or disabled mentions")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"1"}`))}, nil
	})})
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("content", strings.Repeat("💸", maxMessageRunes))
	_ = form.Close()
	request := httptest.NewRequest(http.MethodPost, "/send", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-Webhook-Password", base64.StdEncoding.EncodeToString([]byte("secret")))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || calls != 1 {
		t.Fatalf("status=%d webhook calls=%d; want 200 and one send", response.Code, calls)
	}
}

func TestWebhookUploadAccepts20MiBAndRejectsTheNextByte(t *testing.T) {
	calls := 0
	handler := newHandler("secret", "https://discord.com/api/webhooks/123/secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"1"}`))}, nil
	})})
	for _, tc := range []struct {
		size int64
		want int
	}{{maxFileBytes, http.StatusOK}, {maxFileBytes + 1, http.StatusRequestEntityTooLarge}} {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		_, _ = form.CreateFormField("content")
		part, err := form.CreateFormFile("file", "cat.txt")
		if err != nil {
			t.Fatal(err)
		}
		chunk := bytes.Repeat([]byte("x"), 32<<10)
		for written := int64(0); written < tc.size; {
			n := min(int64(len(chunk)), tc.size-written)
			if _, err := part.Write(chunk[:n]); err != nil {
				t.Fatal(err)
			}
			written += n
		}
		_ = form.Close()
		request := httptest.NewRequest(http.MethodPost, "/send", &body)
		request.Header.Set("Content-Type", form.FormDataContentType())
		request.Header.Set("X-Webhook-Password", base64.StdEncoding.EncodeToString([]byte("secret")))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.want {
			t.Fatalf("file size %d status=%d, want %d", tc.size, response.Code, tc.want)
		}
	}
	if calls != 1 {
		t.Fatalf("webhook calls=%d, want only the allowed file sent", calls)
	}
}

func TestWebhookSendRateLimitsWrongPasswordsAndRejectsOtherOrigins(t *testing.T) {
	calls := 0
	handler := newHandler("secret", "https://discord.com/api/webhooks/123/secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, nil
	})})
	for range maxPasswordFails {
		request := httptest.NewRequest(http.MethodPost, "/send", nil)
		request.Header.Set("X-Webhook-Password", base64.StdEncoding.EncodeToString([]byte("wrong")))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("failed login status=%d, want 401", response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/send", nil)
	request.Header.Set("X-Webhook-Password", base64.StdEncoding.EncodeToString([]byte("secret")))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || calls != 0 {
		t.Fatalf("locked login status=%d webhook calls=%d, want 429 and no send", response.Code, calls)
	}

	request = httptest.NewRequest(http.MethodPost, "/send", nil)
	request.Header.Set("Origin", "https://attacker.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || calls != 0 {
		t.Fatalf("cross-origin request status=%d webhook calls=%d, want 403 and no send", response.Code, calls)
	}
}

func TestWebhookSendRejectsInvalidRequestsWithoutCallingWebhook(t *testing.T) {
	calls := 0
	handler := newHandler("secret", "https://discord.com/api/webhooks/123/secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"1"}`))}, nil
	})})
	for _, tc := range []struct {
		name, content string
		want          int
	}{
		{"empty", "", http.StatusBadRequest},
		{"too long", strings.Repeat("a", maxMessageRunes+1), http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			_ = form.WriteField("content", tc.content)
			_ = form.Close()
			request := httptest.NewRequest(http.MethodPost, "/send", &body)
			request.Header.Set("Content-Type", form.FormDataContentType())
			request.Header.Set("X-Webhook-Password", base64.StdEncoding.EncodeToString([]byte("secret")))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want || calls != 0 {
				t.Fatalf("status=%d webhook calls=%d, want status=%d and no send", response.Code, calls, tc.want)
			}
		})
	}
}

func TestWebhookSendMapsDiscordErrorsAndDoesNotLeakResponses(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		retryAfter string
		err        error
		wantStatus int
		wantWait   int
	}{
		{"rejected", http.StatusBadRequest, `{"error":"confidential-upstream-body"}`, "", nil, http.StatusBadGateway, 0},
		{"rate limited", http.StatusTooManyRequests, `{}`, "7201.4", nil, http.StatusTooManyRequests, 7202},
		{"bad confirmation", http.StatusOK, `{}`, "", nil, http.StatusBadGateway, 0},
		{"timeout", 0, "", "", context.DeadlineExceeded, http.StatusGatewayTimeout, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := newHandler("secret", "https://discord.com/api/webhooks/123/secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				header := make(http.Header)
				header.Set("Retry-After", tc.retryAfter)
				return &http.Response{StatusCode: tc.status, Header: header, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})})
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			_ = form.WriteField("content", "hello")
			_ = form.Close()
			request := httptest.NewRequest(http.MethodPost, "/send", &body)
			request.Header.Set("Content-Type", form.FormDataContentType())
			request.Header.Set("X-Webhook-Password", base64.StdEncoding.EncodeToString([]byte("secret")))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.wantStatus || strings.Contains(response.Body.String(), "confidential-upstream-body") {
				t.Fatalf("response status=%d body=%s; want status=%d without upstream body", response.Code, response.Body.String(), tc.wantStatus)
			}
			if tc.wantWait > 0 && (response.Header().Get("Retry-After") != "7202" || !strings.Contains(response.Body.String(), `"retry_after":7202`)) {
				t.Fatalf("rate-limit response headers/body = %v / %s", response.Header(), response.Body.String())
			}
		})
	}
}

func TestWebhookUploadBodyLimitAndTempFiles(t *testing.T) {
	t.Run("request body limit", func(t *testing.T) {
		calls := 0
		handler := newHandler("secret", "https://discord.com/api/webhooks/123/secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"1"}`))}, nil
		})})
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("file", "large.bin")
		chunk := bytes.Repeat([]byte("x"), 32<<10)
		for written := 0; written <= maxRequestBytes; {
			n := min(len(chunk), maxRequestBytes+1-written)
			_, _ = part.Write(chunk[:n])
			written += n
		}
		_ = form.Close()
		request := httptest.NewRequest(http.MethodPost, "/send", &body)
		request.Header.Set("Content-Type", form.FormDataContentType())
		request.Header.Set("X-Webhook-Password", base64.StdEncoding.EncodeToString([]byte("secret")))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusRequestEntityTooLarge || calls != 0 {
			t.Fatalf("oversized body status=%d webhook calls=%d; want 413 and no send", response.Code, calls)
		}
	})

	t.Run("temporary files cleaned after success and rejection", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("TMPDIR", tempDir)
		for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
			handler := newHandler("secret", "https://discord.com/api/webhooks/123/secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"1"}`))}, nil
			})})
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			_ = form.WriteField("content", "hello")
			part, _ := form.CreateFormFile("file", "cat.txt")
			chunk := bytes.Repeat([]byte("x"), 32<<10)
			for range 64 {
				_, _ = part.Write(chunk)
			}
			_ = form.Close()
			request := httptest.NewRequest(http.MethodPost, "/send", &body)
			request.Header.Set("Content-Type", form.FormDataContentType())
			request.Header.Set("X-Webhook-Password", base64.StdEncoding.EncodeToString([]byte("secret")))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			entries, err := os.ReadDir(tempDir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary files after HTTP %d response: %v, err=%v", status, entries, err)
			}
		}
	})
}

func TestValidWebhookURL(t *testing.T) {
	for _, tc := range []struct {
		url string
		ok  bool
	}{
		{"https://discord.com/api/webhooks/123/secret-token", true},
		{"http://discord.com/api/webhooks/123/token", false},
		{"https://evil.example/api/webhooks/123/token", false},
		{"https://discord.com.evil.example/api/webhooks/123/token", false},
		{"https://discord.com/api/webhooks/123/token?redirect=evil", false},
		{"https://discord.com/api/webhooks/123/token/extra", false},
		{"", false},
	} {
		if got := validWebhookURL(tc.url); got != tc.ok {
			t.Errorf("validWebhookURL(%q)=%t, want %t", tc.url, got, tc.ok)
		}
	}
}

func TestPageAllowsSameOriginRequests(t *testing.T) {
	response := httptest.NewRecorder()
	newHandler("secret", "https://discord.com/api/webhooks/123/secret", nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	policy := response.Header().Get("Content-Security-Policy")
	if response.Code != http.StatusOK || !strings.Contains(policy, "connect-src 'self'") {
		t.Fatalf("page response status=%d CSP=%q; same-origin submission must be allowed", response.Code, policy)
	}
	if !strings.Contains(response.Body.String(), `id="remove-file"`) || !strings.Contains(response.Body.String(), `<svg class="pixel-art"`) {
		t.Fatal("page lacks the file removal control or pixel-art Nyan Cat")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
