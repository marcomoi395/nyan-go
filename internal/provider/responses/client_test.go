package responses

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"nyan-go/internal/app"
)

func TestRespondSendsCurrentMessageAndFixedTools(t *testing.T) {
	var requestBody responseRequest
	clientHTTP := &http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v1/responses" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization header missing")
		}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			return nil, err
		}
		return jsonResponse(map[string]any{"id": "resp_1", "output": []any{}}), nil
	})}

	client, err := NewClient("http://provider.test/v1", "gpt-test", "secret", WithHTTPClient(clientHTTP))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Respond(context.Background(), app.ProviderRequest{Message: "trưa nay ăn phở 45k"})
	if err != nil {
		t.Fatal(err)
	}
	if !response.NoAction || response.ResponseID != "resp_1" {
		t.Fatalf("response = %+v", response)
	}
	if requestBody.Model != "gpt-test" || requestBody.PreviousResponse != "" {
		t.Fatalf("request = %+v", requestBody)
	}
	input, ok := requestBody.Input.([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input = %#v", requestBody.Input)
	}
	if len(requestBody.Tools) != 7 {
		t.Fatalf("tool count = %d", len(requestBody.Tools))
	}
	for _, tool := range requestBody.Tools {
		if tool.Type != "function" || !tool.Strict || tool.Parameters["additionalProperties"] != false {
			t.Fatalf("tool = %+v", tool)
		}
	}
}

func TestRespondParsesMultipleFunctionCalls(t *testing.T) {
	client, err := NewClient("http://provider.test", "model", "", WithHTTPClient(&http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(map[string]any{
			"id": "resp_1",
			"output": []any{
				map[string]any{"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "create_transaction", "arguments": `{"type":"expense","amount_vnd":45000}`},
				map[string]any{"type": "function_call", "id": "fc_2", "call_id": "call_2", "name": "get_statistics", "arguments": `{"start":"2026-09-01","end":"2026-09-02","grouping":"total"}`},
			},
		}), nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Respond(context.Background(), app.ProviderRequest{Message: "chi tiêu"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.FunctionCalls) != 2 || response.FunctionCalls[1].CallID != "call_2" {
		t.Fatalf("calls = %+v", response.FunctionCalls)
	}
}

func TestRespondIgnoresReasoningOutput(t *testing.T) {
	client, err := NewClient("http://provider.test", "model", "", WithHTTPClient(&http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(map[string]any{
			"id": "resp_1",
			"output": []any{
				map[string]any{"type": "reasoning", "id": "rs_1"},
				map[string]any{
					"type": "message", "role": "assistant",
					"content": []any{map[string]any{"type": "output_text", "text": "Đã ghi"}},
				},
			},
		}), nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Respond(context.Background(), app.ProviderRequest{Message: "ghi chi tiêu"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "Đã ghi" {
		t.Fatalf("text = %q", response.Text)
	}
}

func TestRespondParsesNumericUsage(t *testing.T) {
	client, err := NewClient("http://provider.test", "model", "", WithHTTPClient(&http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(map[string]any{"id": "resp_1", "output_text": "ok", "output": []any{}, "usage": map[string]any{"input_tokens": 4, "output_tokens": 3, "total_tokens": 7}}), nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Respond(context.Background(), app.ProviderRequest{Message: "x"})
	if err != nil || response.Usage == nil || response.Usage.TotalTokens != 7 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

type usageRecorder struct{ record UsageRecord }

func (r *usageRecorder) RecordUsage(record UsageRecord) { r.record = record }

func TestRespondRecordsNumericUsageWithoutMessageContent(t *testing.T) {
	recorder := &usageRecorder{}
	client, err := NewClient("http://provider.test", "model", "", WithUsageRecorder(recorder), WithHTTPClient(&http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(map[string]any{"id": "resp_1", "output": []any{}, "usage": map[string]any{"input_tokens": 4, "output_tokens": 3, "total_tokens": 7}}), nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Respond(context.Background(), app.ProviderRequest{Message: "private note 25k"}); err != nil {
		t.Fatal(err)
	}
	if recorder.record.TotalTokens != 7 || recorder.record.RoundCount != 1 || recorder.record.Action != "respond" || recorder.record.Status != "success" {
		t.Fatalf("record=%+v", recorder.record)
	}
}

func TestRespondWithDispatcherContinuesWithOnlyFunctionOutputs(t *testing.T) {
	var requests []responseRequest
	client, err := NewClient("http://provider.test", "model", "", WithHTTPClient(&http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		var body responseRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			return nil, err
		}
		requests = append(requests, body)
		if len(requests) == 1 {
			return jsonResponse(map[string]any{
				"id":     "resp_1",
				"output": []any{map[string]any{"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "search_transactions", "arguments": `{}`}},
			}), nil
		}
		return jsonResponse(map[string]any{"id": "resp_2", "output_text": "Đã xong", "output": []any{}}), nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.RespondWithDispatcher(context.Background(), app.ProviderRequest{Message: "tìm giao dịch"}, func(_ context.Context, call app.FunctionCall) (string, error) {
		if call.Name != "search_transactions" {
			t.Fatalf("call = %+v", call)
		}
		return `{"count":1}`, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "Đã xong" || len(response.FunctionCalls) != 1 {
		t.Fatalf("response = %+v", response)
	}
	if len(requests) != 2 || requests[1].PreviousResponse != "resp_1" {
		t.Fatalf("requests = %+v", requests)
	}
	if strings.Contains(requestBody(requests[1]), "tìm giao dịch") {
		t.Fatal("continuation repeated the original user message")
	}
}

func TestRespondWithDispatcherAggregatesUsageAcrossRounds(t *testing.T) {
	recorder := &usageRecorder{}
	requests := 0
	client, err := NewClient("http://provider.test", "model", "", WithUsageRecorder(recorder), WithHTTPClient(&http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return jsonResponse(map[string]any{"id": "resp_1", "output": []any{map[string]any{"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "search_transactions", "arguments": `{}`}}, "usage": map[string]any{"input_tokens": 2, "output_tokens": 1, "total_tokens": 3}}), nil
		}
		return jsonResponse(map[string]any{"id": "resp_2", "output": []any{}, "usage": map[string]any{"input_tokens": 4, "output_tokens": 3, "total_tokens": 7}}), nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.RespondWithDispatcher(context.Background(), app.ProviderRequest{Message: "tìm"}, func(context.Context, app.FunctionCall) (string, error) { return `{}`, nil }); err != nil {
		t.Fatal(err)
	}
	if recorder.record.InputTokens != 6 || recorder.record.OutputTokens != 4 || recorder.record.TotalTokens != 10 || recorder.record.RoundCount != 2 || recorder.record.Status != "success" {
		t.Fatalf("record=%+v", recorder.record)
	}
}

func TestRespondWithDispatcherStopsAfterMutationRound(t *testing.T) {
	requests := 0
	client, err := NewClient("http://provider.test", "model", "", WithHTTPClient(&http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		requests++
		if requests > 1 {
			return nil, errors.New("unexpected continuation after mutation")
		}
		return jsonResponse(map[string]any{
			"id":     "resp_1",
			"output": []any{map[string]any{"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "create_transaction", "arguments": `{"type":"expense","amount_vnd":45000,"category":"food"}`}},
		}), nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.RespondWithDispatcher(context.Background(), app.ProviderRequest{Message: "bun mam 45k"}, func(_ context.Context, _ app.FunctionCall) (string, error) {
		return `{"queued":true}`, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || len(response.FunctionCalls) != 1 {
		t.Fatalf("requests=%d response=%+v", requests, response)
	}
}

func TestRespondRejectsUnsupportedOutputAndMalformedArguments(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "unsupported", body: `{"id":"resp","output":[{"type":"computer_call"}]}`, want: ErrUnsupportedResponse},
		{name: "unknown tool", body: `{"id":"resp","output":[{"type":"function_call","id":"fc","call_id":"call","name":"arbitrary","arguments":"{}"}]}`, want: ErrInvalidToolCall},
		{name: "bad arguments", body: `{"id":"resp","output":[{"type":"function_call","id":"fc","call_id":"call","name":"search_transactions","arguments":"[]"}]}`, want: ErrInvalidToolCall},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient("http://provider.test", "model", "", WithHTTPClient(&http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header)}, nil
			})}))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Respond(context.Background(), app.ProviderRequest{Message: "x"})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (r roundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return r(request) }

func jsonResponse(value any) *http.Response {
	encoded, _ := json.Marshal(value)
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(encoded))), Header: make(http.Header)}
}

func requestBody(request responseRequest) string {
	encoded, _ := json.Marshal(request.Input)
	return string(encoded)
}
