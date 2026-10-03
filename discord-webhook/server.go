package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxFileBytes     = 20 << 20
	maxMessageRunes  = 2000
	maxRequestBytes  = maxFileBytes + (64 << 10)
	maxMemoryBytes   = 1 << 20
	maxWebhookReply  = 1 << 20
	maxConcurrent    = 2
	maxPasswordFails = 5
	passwordWindow   = time.Minute
)

type webhookSender struct {
	passwordHash [sha256.Size]byte
	endpoint     string
	client       *http.Client
	active       chan struct{}
	mu           sync.Mutex
	failures     map[string]failureWindow
}

type failureWindow struct {
	count int
	until time.Time
}

func newHandler(password, webhookURL string, client *http.Client) http.Handler {
	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	if clientCopy.Timeout <= 0 {
		clientCopy.Timeout = 30 * time.Second
	}
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	sender := &webhookSender{
		passwordHash: sha256.Sum256([]byte(password)), endpoint: webhookURL, client: &clientCopy,
		active: make(chan struct{}, maxConcurrent), failures: make(map[string]failureWindow),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		_, _ = io.WriteString(w, pageHTML)
	})
	mux.HandleFunc("POST /send", sender.handleSend)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.EqualFold(parsed.Host, r.Host) || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				writeError(w, http.StatusForbidden, "Yêu cầu từ trang khác bị từ chối.", 0)
				return
			}
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			writeError(w, http.StatusForbidden, "Yêu cầu từ trang khác bị từ chối.", 0)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *webhookSender) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Phương thức không được hỗ trợ.", 0)
		return
	}
	if !s.allowPasswordAttempt(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "Quá nhiều lần nhập sai. Hãy chờ một phút.", 60)
		return
	}
	select {
	case s.active <- struct{}{}:
		defer func() { <-s.active }()
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "Đang gửi tin khác. Hãy thử lại sau giây lát.", 1)
		return
	}
	suppliedPasswordBytes, decodeErr := base64.StdEncoding.DecodeString(r.Header.Get("X-Webhook-Password"))
	if decodeErr != nil {
		suppliedPasswordBytes = nil
	}
	suppliedPassword := string(suppliedPasswordBytes)
	suppliedPasswordHash := sha256.Sum256([]byte(suppliedPassword))
	if subtle.ConstantTimeCompare(suppliedPasswordHash[:], s.passwordHash[:]) != 1 {
		s.recordPasswordFailure(clientIP(r))
		writeError(w, http.StatusUnauthorized, "Mật khẩu chưa đúng.", 0)
		return
	}
	s.clearPasswordFailures(clientIP(r))
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	parseErr := r.ParseMultipartForm(maxMemoryBytes)
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if parseErr != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(parseErr, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "Tệp hoặc yêu cầu vượt giới hạn 20 MiB.", 0)
			return
		}
		writeError(w, http.StatusBadRequest, "Dữ liệu gửi lên không hợp lệ.", 0)
		return
	}
	content := r.FormValue("content")
	if !utf8.ValidString(content) || utf8.RuneCountInString(content) > maxMessageRunes {
		writeError(w, http.StatusBadRequest, "Tin nhắn tối đa 2.000 ký tự.", 0)
		return
	}
	if len(r.MultipartForm.File["file"]) > 1 {
		writeError(w, http.StatusBadRequest, "Mỗi lần chỉ gửi được một tệp.", 0)
		return
	}
	for key, values := range r.MultipartForm.Value {
		if key != "content" || len(values) != 1 {
			writeError(w, http.StatusBadRequest, "Biểu mẫu gửi lên không hợp lệ.", 0)
			return
		}
	}
	for key := range r.MultipartForm.File {
		if key != "file" {
			writeError(w, http.StatusBadRequest, "Biểu mẫu gửi lên không hợp lệ.", 0)
			return
		}
	}
	var attachment *multipart.FileHeader
	if files := r.MultipartForm.File["file"]; len(files) == 1 {
		attachment = files[0]
		if attachment.Size > maxFileBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "Tệp vượt giới hạn 20 MiB.", 0)
			return
		}
	}
	if strings.TrimSpace(content) == "" && attachment == nil {
		writeError(w, http.StatusBadRequest, "Nhập tin nhắn hoặc chọn một tệp.", 0)
		return
	}
	if err := s.send(r.Context(), content, attachment); err != nil {
		var rateLimit *rateLimitError
		if errors.As(err, &rateLimit) {
			w.Header().Set("Retry-After", strconv.Itoa(rateLimit.seconds))
			writeError(w, http.StatusTooManyRequests, "Discord đang giới hạn gửi. Vui lòng chờ rồi thử lại.", rateLimit.seconds)
			return
		}
		var responseErr *webhookResponseError
		if errors.As(err, &responseErr) {
			writeError(w, http.StatusBadGateway, "Discord từ chối tin nhắn hoặc tệp.", 0)
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "Discord chưa xác nhận. Kiểm tra kênh trước khi gửi lại.", 0)
			return
		}
		writeError(w, http.StatusBadGateway, "Không nhận được xác nhận từ Discord. Kiểm tra kênh trước khi gửi lại.", 0)
		return
	}
	writeJSON(w, http.StatusOK, sendResponse{Success: true, Message: "Đã gửi đến Discord."})
}

func (s *webhookSender) send(ctx context.Context, content string, attachment *multipart.FileHeader) error {
	var body io.Reader
	var requestContentType string
	if attachment == nil {
		payload := map[string]any{"content": content, "allowed_mentions": map[string]any{"parse": []string{}}}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(encoded))
		requestContentType = "application/json"
	} else {
		file, err := attachment.Open()
		if err != nil {
			return err
		}
		defer file.Close()
		var data bytes.Buffer
		writer := multipart.NewWriter(&data)
		payload := map[string]any{"content": content, "allowed_mentions": map[string]any{"parse": []string{}}, "attachments": []any{map[string]any{"id": 0, "filename": cleanFilename(attachment.Filename)}}}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if err = writer.WriteField("payload_json", string(encoded)); err != nil {
			return err
		}
		part, err := writer.CreateFormFile("files[0]", cleanFilename(attachment.Filename))
		if err != nil {
			return err
		}
		copied, err := io.Copy(part, io.LimitReader(file, maxFileBytes+1))
		if err != nil {
			return err
		}
		if copied > maxFileBytes {
			return errors.New("file exceeds upload limit")
		}
		if err = writer.Close(); err != nil {
			return err
		}
		body, requestContentType = &data, writer.FormDataContentType()
	}
	requestURL := s.endpoint + "?wait=true"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", requestContentType)
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return &rateLimitError{seconds: retryAfter(response.Header.Get("Retry-After"))}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &webhookResponseError{status: response.StatusCode}
	}
	if response.StatusCode == http.StatusNoContent {
		return errors.New("Discord did not return delivery confirmation")
	}
	reply, err := io.ReadAll(io.LimitReader(response.Body, maxWebhookReply+1))
	if err != nil || len(reply) > maxWebhookReply {
		return errors.New("Discord response exceeded the limit")
	}
	var message struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(reply, &message) != nil || message.ID == "" {
		return errors.New("Discord returned an invalid delivery confirmation")
	}
	return nil
}

func (s *webhookSender) allowPasswordAttempt(ip string) bool {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for address, window := range s.failures {
		if !now.Before(window.until) {
			delete(s.failures, address)
		}
	}
	window := s.failures[ip]
	return window.count < maxPasswordFails || !now.Before(window.until)
}

func (s *webhookSender) clearPasswordFailures(ip string) {
	s.mu.Lock()
	delete(s.failures, ip)
	s.mu.Unlock()
}

func (s *webhookSender) recordPasswordFailure(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	window := s.failures[ip]
	if !now.Before(window.until) {
		window = failureWindow{until: now.Add(passwordWindow)}
	}
	window.count++
	if len(s.failures) >= 1024 {
		for address := range s.failures {
			delete(s.failures, address)
			break
		}
	}
	s.failures[ip] = window
}

func clientIP(r *http.Request) string {
	address, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return address
}

func cleanFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	var safe strings.Builder
	for _, character := range name {
		if !unicode.IsControl(character) {
			safe.WriteRune(character)
		}
	}
	name = strings.TrimSpace(safe.String())
	if name == "" || name == "." || name == "/" {
		return "upload"
	}
	if len(name) > 240 {
		for len(name) > 240 {
			_, size := utf8.DecodeLastRuneInString(name)
			name = name[:len(name)-size]
		}
	}
	return name
}

func retryAfter(value string) int {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds > 0 {
		return seconds
	}
	if seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && seconds > 0 {
		if seconds >= float64(math.MaxInt) {
			return math.MaxInt
		}
		return max(int(math.Ceil(seconds)), 1)
	}
	if timestamp, err := http.ParseTime(value); err == nil {
		seconds := time.Until(timestamp).Seconds()
		if seconds >= float64(math.MaxInt) {
			return math.MaxInt
		}
		return max(int(seconds+0.999), 1)
	}
	return 1
}

func writeError(w http.ResponseWriter, status int, message string, retry int) {
	writeJSON(w, status, sendResponse{Message: message, RetryAfter: retry})
}

func writeJSON(w http.ResponseWriter, status int, value sendResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type sendResponse struct {
	Success    bool   `json:"success,omitempty"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retry_after,omitempty"`
}

type rateLimitError struct{ seconds int }

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("Discord rate limited sending for %d seconds", e.seconds)
}

type webhookResponseError struct{ status int }

func (e *webhookResponseError) Error() string {
	return fmt.Sprintf("Discord returned HTTP %d", e.status)
}

const pageHTML = `<!doctype html>
<html lang="vi"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Nyan Post</title>
<style>
:root{color-scheme:light;--ink:#33264b;--pink:#ff72ad;--blue:#68d7f2;--paper:#fffafc;--edge:#43315e}*{box-sizing:border-box}body{min-height:100vh;margin:0;display:grid;place-items:center;padding:24px;background-color:#c7f2fc;background-image:linear-gradient(90deg,#fff7 50%,transparent 50%),linear-gradient(#fff7 50%,transparent 50%);background-size:8px 8px;color:var(--ink);font-family:ui-monospace,SFMono-Regular,Consolas,monospace}.card{width:min(100%,560px);padding:26px;background:var(--paper);border:4px solid var(--edge);box-shadow:8px 8px 0 #69538b;border-radius:4px}h1{margin:0;text-align:center;font-size:clamp(25px,7vw,38px);letter-spacing:2px;text-shadow:3px 3px #ffc6df}.rainbow{height:9px;margin:20px 0;background:linear-gradient(#ff657d 0 16%,#ffb454 16% 32%,#ffe269 32% 48%,#61d68a 48% 64%,#63bee7 64% 80%,#a87ae0 80%)}.cat{display:flex;align-items:center;justify-content:center;margin:14px}.pixel-art{display:block;width:min(100%,240px);height:auto;image-rendering:pixelated}form{display:grid;gap:15px}label{display:grid;gap:7px;font-weight:700}input,textarea,button{font:inherit;color:inherit;border:3px solid var(--edge);border-radius:2px;padding:11px;background:white}textarea{resize:vertical;min-height:110px}input[type=file]{width:100%;padding:8px;font-size:14px}button{font-weight:900;background:#ff90c3;box-shadow:4px 4px 0 var(--edge);cursor:pointer}button:focus-visible,input:focus-visible,textarea:focus-visible{outline:3px solid #568bd7;outline-offset:3px}button:disabled{opacity:.6;cursor:wait;box-shadow:none}.hint{font-size:13px;margin:0}.status{min-height:1.5em;font-weight:700}.status[data-kind=error]{color:#a42648}.status[data-kind=success]{color:#176d40}@media(max-width:480px){.card{padding:18px}body{padding:12px}}
</style></head><body><main class="card"><h1>NYAN POST</h1><div class="rainbow" aria-hidden="true"></div><div class="cat"><svg class="pixel-art" viewBox="0 0 180 108" role="img" aria-label="Nyan Cat vẽ bằng pixel" shape-rendering="crispEdges"><rect x="0" y="42" width="36" height="8" fill="#ff657d"/><rect x="0" y="50" width="36" height="8" fill="#ffb454"/><rect x="0" y="58" width="36" height="8" fill="#ffe269"/><rect x="0" y="66" width="36" height="8" fill="#61d68a"/><rect x="0" y="74" width="36" height="8" fill="#63bee7"/><rect x="0" y="82" width="36" height="8" fill="#a87ae0"/><rect x="30" y="30" width="84" height="60" fill="#43315e"/><rect x="90" y="84" width="12" height="12" fill="#43315e"/><rect x="114" y="84" width="12" height="12" fill="#43315e"/><rect x="102" y="24" width="60" height="60" fill="#43315e"/><rect x="108" y="6" width="18" height="30" fill="#43315e"/><rect x="138" y="6" width="18" height="30" fill="#43315e"/><rect x="36" y="36" width="72" height="48" fill="#ff9cc7"/><rect x="48" y="48" width="12" height="6" fill="#fff1a8"/><rect x="72" y="48" width="12" height="6" fill="#fff1a8"/><rect x="60" y="66" width="24" height="6" fill="#fff1a8"/><rect x="108" y="30" width="48" height="48" fill="#d8d5e4"/><rect x="114" y="12" width="6" height="12" fill="#e998b4"/><rect x="144" y="12" width="6" height="12" fill="#e998b4"/><rect x="120" y="42" width="6" height="6" fill="#43315e"/><rect x="138" y="42" width="6" height="6" fill="#43315e"/><rect x="132" y="48" width="6" height="6" fill="#ff72ad"/><rect x="108" y="60" width="6" height="6" fill="#fffafc"/><rect x="150" y="60" width="6" height="6" fill="#fffafc"/></svg></div>
<form id="sender"><label>Mật khẩu<input id="password" type="password" autocomplete="current-password" required></label><label>Tin nhắn<textarea id="content" maxlength="4000" placeholder="Gửi lời chào..." aria-describedby="count"></textarea></label><p class="hint" id="count" aria-live="polite">0 / 2.000 ký tự</p><label>Tệp đính kèm (tối đa 20 MiB)<input id="file" type="file"><span id="file-name" class="hint"></span></label><button id="remove-file" type="button" disabled>Bỏ tệp đã chọn</button><p class="hint">Thông báo @everyone và mention đã tắt.</p><button id="send" type="submit">Gửi tới Discord ✦</button><p id="status" class="status" role="status" aria-live="polite"></p></form></main>
<script>
const form=document.querySelector('#sender'),password=document.querySelector('#password'),content=document.querySelector('#content'),file=document.querySelector('#file'),removeFile=document.querySelector('#remove-file'),send=document.querySelector('#send'),status=document.querySelector('#status'),count=document.querySelector('#count'),fileName=document.querySelector('#file-name');
const limit=2000,maxFile=20*1024*1024;content.addEventListener('input',()=>{const n=Array.from(content.value).length;count.textContent=n+' / 2.000 ký tự';count.style.color=n>limit?'#a42648':''});file.addEventListener('change',()=>{const f=file.files[0];fileName.textContent=f?f.name+' · '+(f.size/1048576).toFixed(2)+' MiB':'';removeFile.disabled=!f});removeFile.addEventListener('click',()=>{file.value='';file.dispatchEvent(new Event('change'))});
form.addEventListener('submit',async event=>{event.preventDefault();const selected=file.files[0],n=Array.from(content.value).length;if(!password.value){status.dataset.kind='error';status.textContent='Nhập mật khẩu trước khi gửi.';password.focus();return}if(!content.value.trim()&&!selected){status.dataset.kind='error';status.textContent='Nhập tin nhắn hoặc chọn một tệp.';return}if(n>limit){status.dataset.kind='error';status.textContent='Tin nhắn vượt giới hạn 2.000 ký tự.';content.focus();return}if(selected&&selected.size>maxFile){status.dataset.kind='error';status.textContent='Tệp vượt giới hạn 20 MiB.';return}const data=new FormData();data.set('content',content.value);if(selected)data.set('file',selected,selected.name);const encodedPassword=btoa(Array.from(new TextEncoder().encode(password.value),byte=>String.fromCharCode(byte)).join(''));send.disabled=true;status.dataset.kind='';status.textContent='Đang gửi…';try{const response=await fetch('/send',{method:'POST',headers:{'X-Webhook-Password':encodedPassword},body:data});const result=await response.json();if(!response.ok)throw Object.assign(new Error(result.message||'Không gửi được tin nhắn.'),{retry:result.retry_after});status.dataset.kind='success';status.textContent=result.message;content.value='';file.value='';removeFile.disabled=true;count.textContent='0 / 2.000 ký tự';fileName.textContent=''}catch(error){status.dataset.kind='error';status.textContent=error.message+(error.retry?' Chờ '+error.retry+' giây rồi thử lại.':'')}finally{send.disabled=false}});
</script></body></html>`
