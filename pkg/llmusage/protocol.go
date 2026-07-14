package llmusage

// Protocol identifies a response wire contract. It does not identify the
// company that served an OpenAI-compatible response.
type Protocol string

const (
	ProtocolAuto                  Protocol = "auto"
	ProtocolOpenAIResponses       Protocol = "openai.responses"
	ProtocolOpenAIChatCompletions Protocol = "openai.chat-completions"
	ProtocolAnthropicMessages     Protocol = "anthropic.messages"
	ProtocolGoogleGenerateContent Protocol = "google.generate-content"
)

// Format identifies the framing used by a response body.
type Format string

const (
	FormatJSON Format = "json"
	FormatSSE  Format = "sse"
)
