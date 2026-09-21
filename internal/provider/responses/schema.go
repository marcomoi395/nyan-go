package responses

import "encoding/json"

// responseRequest is the subset of the Responses request contract used by
// this provider. Input is deliberately either the user's current message or
// the active function outputs, never conversation history.
type responseRequest struct {
	Model            string        `json:"model"`
	Instructions     string        `json:"instructions,omitempty"`
	Input            any           `json:"input"`
	Tools            []requestTool `json:"tools"`
	PreviousResponse string        `json:"previous_response_id,omitempty"`
}

type requestTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict"`
}

type userInput struct {
	Role    string         `json:"role"`
	Content []inputContent `json:"content"`
}

type inputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type functionOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

type responsePayload struct {
	ID         string            `json:"id"`
	Output     []json.RawMessage `json:"output"`
	OutputText string            `json:"output_text"`
	Usage      *responseUsage    `json:"usage"`
}

type responseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type outputHeader struct {
	Type string `json:"type"`
}

type responseFunctionCall struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type responseMessage struct {
	Type    string            `json:"type"`
	Role    string            `json:"role"`
	Content []responseContent `json:"content"`
}

type responseContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
