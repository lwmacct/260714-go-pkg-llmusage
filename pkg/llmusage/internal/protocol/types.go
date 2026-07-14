package protocol

import "encoding/json"

type Kind string

const (
	Auto                  Kind = "auto"
	OpenAIResponses       Kind = "openai.responses"
	OpenAIChatCompletions Kind = "openai.chat-completions"
	AnthropicMessages     Kind = "anthropic.messages"
	GoogleGenerateContent Kind = "google.generate-content"
)

type Limits struct {
	MaxResultBytes  int
	MaxNestingDepth int
}

type Event struct {
	Sequence uint64
	Type     string
}

type Usage struct {
	InputTokens       int64
	OutputTokens      int64
	TotalTokens       int64
	CachedInputTokens int64
	CacheWriteTokens  int64
	ReasoningTokens   int64
}

type TotalSource string

const (
	TotalReported TotalSource = "reported"
	TotalDerived  TotalSource = "derived"
	TotalUnknown  TotalSource = "unknown"
)

type Result struct {
	Protocol    Kind
	ResponseID  string
	Model       string
	Usage       Usage
	TotalSource TotalSource
	RawUsage    json.RawMessage
	Sequence    uint64
}

type JSONDecoder interface {
	Feed([]byte) error
	Finish() ([]Result, error)
}

type SSEDecoder interface {
	FeedEventData([]byte) error
	FinishEvent(Event) ([]Result, error)
	FinishStream() ([]Result, error)
}
