// Package anthropic implements the Provider interface for Anthropic's Claude models.
// There is no official Go SDK, so we implement the Messages API directly.
// API reference: https://docs.anthropic.com/en/api/messages
package anthropic

import "encoding/json"

const (
	apiVersion     = "2023-06-01"
	defaultBaseURL = "https://api.anthropic.com"
)

// ---- Request types ---------------------------------------------------------

// messageRequest is the Anthropic Messages API request body.
type messageRequest struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	System    string    `json:"system,omitempty"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream,omitempty"`
	// Sampling parameters
	Temperature float32  `json:"temperature,omitempty"`
	TopP        float32  `json:"top_p,omitempty"`
	TopK        int      `json:"top_k,omitempty"`
	StopSeqs    []string `json:"stop_sequences,omitempty"`
}

// message is a single turn in an Anthropic conversation.
// Note: Anthropic only supports "user" and "assistant" roles (no "system").
// System prompts are extracted from the messages slice and placed in the top-level
// system field.
type message struct {
	Role    string         `json:"role"` // "user" | "assistant"
	Content messageContent `json:"content"`
}

// messageContent can be either a plain string or a slice of content blocks.
// We use json.RawMessage for flexibility during unmarshalling.
type messageContent = json.RawMessage

// textContent is the most common content block type.
type textContent struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// ---- Response types --------------------------------------------------------

// messageResponse is the Anthropic Messages API response body.
type messageResponse struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`   // "message"
	Role         string    `json:"role"`   // "assistant"
	Content      []content `json:"content"` // array of content blocks
	Model        string    `json:"model"`
	StopReason   string    `json:"stop_reason"`   // "end_turn" | "max_tokens" | "stop_sequence" | "tool_use"
	StopSequence *string   `json:"stop_sequence"` // set if stop_reason=="stop_sequence"
	Usage        usage     `json:"usage"`
}

// content is a content block in the response.
type content struct {
	Type string `json:"type"` // "text" | "tool_use"
	Text string `json:"text,omitempty"`
}

// usage holds token counts.
type usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ---- Streaming SSE types ---------------------------------------------------

// streamEvent is the parsed form of an Anthropic SSE event.
type streamEvent struct {
	Type string `json:"type"`
}

// contentBlockDelta is sent as the model generates text.
type contentBlockDelta struct {
	Type  string `json:"type"`  // "content_block_delta"
	Index int    `json:"index"`
	Delta delta  `json:"delta"`
}

type delta struct {
	Type string `json:"type"` // "text_delta"
	Text string `json:"text"`
}

// messageDelta is sent at stream end with usage stats.
type messageDelta struct {
	Type  string `json:"type"` // "message_delta"
	Delta struct {
		StopReason   string  `json:"stop_reason"`
		StopSequence *string `json:"stop_sequence"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// stopReasonToFinishReason maps Anthropic stop reasons to OpenAI finish reasons.
func stopReasonToFinishReason(r string) string {
	switch r {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}
