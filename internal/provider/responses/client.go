// Package responses implements the OpenAI-compatible Responses API transport.
package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nyan-go/internal/app"
	"nyan-go/internal/config"
)

const (
	defaultTimeout = 30 * time.Second
	defaultRounds  = 4
	maxBodyBytes   = 2 << 20
)

var (
	ErrMalformedResponse   = errors.New("provider returned a malformed response")
	ErrUnsupportedResponse = errors.New("provider returned an unsupported response item")
	ErrProviderFailure     = errors.New("AI provider request failed")
	ErrInvalidToolCall     = errors.New("provider returned an invalid tool call")
	ErrRoundLimit          = errors.New("provider function-call round limit exceeded")
)

// ProviderError exposes only HTTP metadata; provider bodies may contain
// prompts or financial data and are intentionally discarded.
type ProviderError struct{ StatusCode int }

func (e *ProviderError) Error() string {
	if e.StatusCode == 0 {
		return ErrProviderFailure.Error()
	}
	return fmt.Sprintf("%s (HTTP %d)", ErrProviderFailure, e.StatusCode)
}

func (e *ProviderError) Unwrap() error { return ErrProviderFailure }

type Option func(*Client)

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if timeout > 0 {
			c.timeout = timeout
		}
	}
}

func WithMaxRounds(rounds int) Option {
	return func(c *Client) {
		if rounds > 0 {
			c.maxRounds = rounds
		}
	}
}

// Client is an app.AIProvider backed by a Responses API endpoint.
type Client struct {
	baseURL      string
	model        string
	apiKey       string
	httpClient   *http.Client
	timeout      time.Duration
	maxRounds    int
	instructions string
}

func NewClient(baseURL, model, apiKey string, options ...Option) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("invalid provider base URL")
	}
	if u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(u.Path, "/responses") {
		return nil, errors.New("provider base URL must be an API root")
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("provider model is required")
	}
	c := &Client{
		baseURL:      baseURL,
		model:        strings.TrimSpace(model),
		apiKey:       strings.TrimSpace(apiKey),
		httpClient:   http.DefaultClient,
		timeout:      defaultTimeout,
		maxRounds:    defaultRounds,
		instructions: systemInstructions,
	}
	for _, option := range options {
		if option != nil {
			option(c)
		}
	}
	return c, nil
}

func NewFromConfig(cfg config.Config, options ...Option) (*Client, error) {
	return NewClient(cfg.OpenAIBaseURL, cfg.OpenAIModel, cfg.OpenAIAPIKey, options...)
}

// FixedTools returns a new copy of the seven tools exposed to the model.
func FixedTools() []app.ToolDefinition {
	tools := []toolSchema{
		{
			name: "create_transaction", description: "Tạo một giao dịch thu nhập hoặc chi tiêu.",
			properties: map[string]any{
				"type":       map[string]any{"type": "string", "enum": []string{"income", "expense"}},
				"amount_vnd": map[string]any{"type": "integer", "minimum": 1},
				"category":   map[string]any{"type": "string"}, "note": map[string]any{"type": "string"},
				"occurred_at": map[string]any{"type": "string"},
			}, required: []string{"type", "amount_vnd", "category", "note", "occurred_at"},
		},
		{
			name: "update_transaction", description: "Cập nhật giao dịch đã xác định rõ.",
			properties: map[string]any{
				"transaction_id": map[string]any{"type": "integer", "minimum": 1},
				"type":           map[string]any{"type": "string", "enum": []string{"income", "expense"}},
				"amount_vnd":     map[string]any{"type": "integer", "minimum": 1}, "category": map[string]any{"type": "string"},
				"note": map[string]any{"type": "string"}, "occurred_at": map[string]any{"type": "string"},
			}, required: []string{"transaction_id"},
		},
		{
			name: "delete_transaction", description: "Chuẩn bị xác nhận xóa giao dịch.",
			properties: map[string]any{"transaction_id": map[string]any{"type": "integer", "minimum": 1}},
			required:   []string{"transaction_id"},
		},
		{
			name: "restore_transaction", description: "Khôi phục giao dịch đã xóa mềm.",
			properties: map[string]any{"transaction_id": map[string]any{"type": "integer", "minimum": 1}},
			required:   []string{"transaction_id"},
		},
		{
			name: "search_transactions", description: "Tìm giao dịch theo bộ lọc.",
			properties: map[string]any{
				"start": map[string]any{"type": "string"}, "end": map[string]any{"type": "string"},
				"type":           map[string]any{"type": "string", "enum": []string{"income", "expense"}},
				"category":       map[string]any{"type": "string"},
				"min_amount_vnd": map[string]any{"type": "integer", "minimum": 1},
				"max_amount_vnd": map[string]any{"type": "integer", "minimum": 1}, "note": map[string]any{"type": "string"},
			},
		},
		{
			name: "get_statistics", description: "Tính thống kê theo khoảng thời gian.",
			properties: map[string]any{
				"start": map[string]any{"type": "string"}, "end": map[string]any{"type": "string"},
				"type":             map[string]any{"type": "string", "enum": []string{"income", "expense"}},
				"category":         map[string]any{"type": "string"},
				"grouping":         map[string]any{"type": "string", "enum": []string{"total", "day", "category", "type"}},
				"comparison_start": map[string]any{"type": "string"}, "comparison_end": map[string]any{"type": "string"},
			}, required: []string{"start", "end", "grouping"},
		},
		{
			name: "export_transactions", description: "Xuất giao dịch thành CSV.",
			properties: map[string]any{
				"start": map[string]any{"type": "string"}, "end": map[string]any{"type": "string"},
				"type":     map[string]any{"type": "string", "enum": []string{"income", "expense"}},
				"category": map[string]any{"type": "string"},
			},
		},
	}
	result := make([]app.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		parameters := map[string]any{
			"type":                 "object",
			"properties":           tool.properties,
			"required":             tool.required,
			"additionalProperties": false,
		}
		encoded, _ := json.Marshal(parameters)
		result = append(result, app.ToolDefinition{Name: tool.name, Description: tool.description, Parameters: encoded})
	}
	return result
}

type toolSchema struct {
	name        string
	description string
	properties  map[string]any
	required    []string
}

func IsSupportedTool(name string) bool {
	for _, tool := range FixedTools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// Respond performs one provider request. Use RespondWithDispatcher when the
// application needs the provider to continue after executing function calls.
func (c *Client) Respond(ctx context.Context, request app.ProviderRequest) (app.ProviderResponse, error) {
	return c.respond(ctx, request, "", nil)
}

// RespondWithDispatcher runs the Responses function-call continuation loop.
// Tool output is sent to the provider only after the injected dispatcher
// returns it; no conversation history is retained or transmitted.
func (c *Client) RespondWithDispatcher(ctx context.Context, request app.ProviderRequest, dispatch func(context.Context, app.FunctionCall) (string, error)) (app.ProviderResponse, error) {
	if dispatch == nil {
		return app.ProviderResponse{}, errors.New("tool dispatcher is required")
	}
	if c.maxRounds <= 0 {
		return app.ProviderResponse{}, fmt.Errorf("%w: invalid round limit", ErrProviderFailure)
	}
	var previousID string
	var allCalls []app.FunctionCall
	var outputs []functionOutput
	mutationRoundSeen := false
	for round := 0; round < c.maxRounds; round++ {
		response, err := c.respond(ctx, request, previousID, outputs)
		if err != nil {
			return app.ProviderResponse{}, err
		}
		allCalls = append(allCalls, response.FunctionCalls...)
		if len(response.FunctionCalls) == 0 {
			response.FunctionCalls = allCalls
			return response, nil
		}
		outputs = make([]functionOutput, 0, len(response.FunctionCalls))
		mutationInRound := false
		for _, call := range response.FunctionCalls {
			isMutation := isMutationTool(call.Name)
			if mutationInRound && !isMutation {
				return app.ProviderResponse{}, fmt.Errorf("%w: read call after mutation", ErrInvalidToolCall)
			}
			if mutationRoundSeen && isMutation {
				return app.ProviderResponse{}, fmt.Errorf("%w: later mutation batch", ErrInvalidToolCall)
			}
			mutationInRound = mutationInRound || isMutation
			output, err := dispatch(ctx, call)
			if err != nil {
				return app.ProviderResponse{}, err
			}
			outputs = append(outputs, functionOutput{Type: "function_call_output", CallID: call.CallID, Output: output})
		}
		mutationRoundSeen = mutationRoundSeen || mutationInRound
		previousID = response.ResponseID
	}
	return app.ProviderResponse{}, fmt.Errorf("%w: %w", ErrProviderFailure, ErrRoundLimit)
}

func isMutationTool(name string) bool {
	switch name {
	case "create_transaction", "update_transaction", "delete_transaction", "restore_transaction":
		return true
	default:
		return false
	}
}

func (c *Client) respond(ctx context.Context, request app.ProviderRequest, previousID string, outputs []functionOutput) (app.ProviderResponse, error) {
	var input any
	if previousID == "" {
		input = []userInput{{Role: "user", Content: []inputContent{{Type: "input_text", Text: request.Message}}}}
	} else {
		input = outputs
	}
	body := responseRequest{
		Model:            c.model,
		Instructions:     c.instructions,
		Input:            input,
		Tools:            c.requestTools(),
		PreviousResponse: previousID,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return app.ProviderResponse{}, fmt.Errorf("%w: encode request", ErrProviderFailure)
	}
	requestContext := ctx
	if c.timeout > 0 {
		var cancel context.CancelFunc
		requestContext, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	httpRequest, err := http.NewRequestWithContext(requestContext, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(encoded))
	if err != nil {
		return app.ProviderResponse{}, fmt.Errorf("%w: build request", ErrProviderFailure)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		if errors.Is(requestContext.Err(), context.DeadlineExceeded) {
			return app.ProviderResponse{}, context.DeadlineExceeded
		}
		return app.ProviderResponse{}, fmt.Errorf("%w: %v", ErrProviderFailure, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return app.ProviderResponse{}, &ProviderError{StatusCode: response.StatusCode}
	}
	var payload responsePayload
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxBodyBytes))
	if err := decoder.Decode(&payload); err != nil {
		return app.ProviderResponse{}, fmt.Errorf("%w: %v", ErrMalformedResponse, err)
	}
	return payload.toApp()
}

func (c *Client) requestTools() []requestTool {
	tools := FixedTools()
	result := make([]requestTool, 0, len(tools))
	for _, tool := range tools {
		var parameters map[string]any
		if err := json.Unmarshal(tool.Parameters, &parameters); err != nil {
			continue
		}
		result = append(result, requestTool{Type: "function", Name: tool.Name, Description: tool.Description, Parameters: parameters, Strict: true})
	}
	return result
}

func (payload responsePayload) toApp() (app.ProviderResponse, error) {
	if strings.TrimSpace(payload.ID) == "" {
		return app.ProviderResponse{}, fmt.Errorf("%w: missing response id", ErrMalformedResponse)
	}
	if payload.Output == nil && strings.TrimSpace(payload.OutputText) == "" {
		return app.ProviderResponse{}, fmt.Errorf("%w: missing output", ErrMalformedResponse)
	}
	result := app.ProviderResponse{ResponseID: payload.ID}
	var messageText strings.Builder
	for _, raw := range payload.Output {
		var header outputHeader
		if err := json.Unmarshal(raw, &header); err != nil || strings.TrimSpace(header.Type) == "" {
			return app.ProviderResponse{}, fmt.Errorf("%w: invalid output item", ErrMalformedResponse)
		}
		switch header.Type {
		case "function_call":
			var call responseFunctionCall
			if err := json.Unmarshal(raw, &call); err != nil || call.Type != "function_call" || call.ID == "" || call.CallID == "" || !IsSupportedTool(call.Name) {
				return app.ProviderResponse{}, fmt.Errorf("%w: unsupported function call", ErrInvalidToolCall)
			}
			var arguments map[string]any
			if json.Unmarshal([]byte(call.Arguments), &arguments) != nil || arguments == nil {
				return app.ProviderResponse{}, fmt.Errorf("%w: arguments must be a JSON object", ErrInvalidToolCall)
			}
			result.FunctionCalls = append(result.FunctionCalls, app.FunctionCall{ID: call.ID, CallID: call.CallID, ResponseID: payload.ID, Name: call.Name, Arguments: json.RawMessage(call.Arguments)})
		case "message":
			var message responseMessage
			if err := json.Unmarshal(raw, &message); err != nil || message.Role != "assistant" {
				return app.ProviderResponse{}, fmt.Errorf("%w: invalid message", ErrMalformedResponse)
			}
			for _, content := range message.Content {
				if content.Type != "output_text" {
					return app.ProviderResponse{}, fmt.Errorf("%w: unsupported message content", ErrUnsupportedResponse)
				}
				messageText.WriteString(content.Text)
			}
		default:
			return app.ProviderResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedResponse, header.Type)
		}
	}
	result.Text = messageText.String()
	if strings.TrimSpace(result.Text) == "" {
		result.Text = payload.OutputText
	}
	result.NoAction = len(result.FunctionCalls) == 0 && strings.TrimSpace(result.Text) == ""
	return result, nil
}

const systemInstructions = `Bạn là bộ phân tích cho sổ thu chi cá nhân. Chỉ dùng bảy công cụ được cung cấp; không truy cập SQL, không tự đặt danh tính, không tự tạo dữ liệu. Dùng VND và múi giờ Asia/Ho_Chi_Minh. Nếu thiếu hoặc mơ hồ giá trị bắt buộc, hỏi lại bằng tiếng Việt và không gọi công cụ. Chỉ phân tích số liệu do backend cung cấp. Khi phân tích thống kê, trả JSON đúng ba mảng facts, observations, limitations; nêu rõ thiếu bằng chứng trong limitations và không suy đoán nguyên nhân, số liệu, phần trăm, giao dịch hoặc danh mục.`
