package llmusage

import "encoding/json"

// Usage contains provider-neutral token counters. Detail counters are subsets
// of input or output tokens and must not be added to TotalTokens again.
type Usage struct {
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int64 `json:"cache_write_tokens,omitempty"`
	ReasoningTokens   int64 `json:"reasoning_tokens,omitempty"`
}

// TotalSource describes whether TotalTokens came from the response or was
// derived by a protocol-specific rule.
type TotalSource string

const (
	TotalReported TotalSource = "reported"
	TotalDerived  TotalSource = "derived"
	TotalUnknown  TotalSource = "unknown"
)

// Result is one complete usage snapshot extracted from a response.
type Result struct {
	Protocol    Protocol        `json:"protocol"`
	ResponseID  string          `json:"response_id,omitempty"`
	Model       string          `json:"model,omitempty"`
	Usage       Usage           `json:"usage"`
	TotalSource TotalSource     `json:"total_source"`
	RawUsage    json.RawMessage `json:"raw_usage"`
	// Sequence is the one-based SSE data-event sequence. JSON responses use 1.
	Sequence uint64 `json:"sequence"`
}
