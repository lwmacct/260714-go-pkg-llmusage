package protocol

import "fmt"

var ErrUnsupported = fmt.Errorf("unsupported protocol payload")

func NewJSON(kind Kind, limits Limits) (JSONDecoder, error) {
	switch kind {
	case Auto:
		return newAutoJSON(limits), nil
	case OpenAIResponses:
		return newOpenAIResponsesJSON(limits), nil
	case OpenAIChatCompletions:
		return newOpenAIChatJSON(limits), nil
	case AnthropicMessages:
		return newAnthropicJSON(limits), nil
	case GoogleGenerateContent:
		return newGoogleJSON(limits), nil
	default:
		return nil, ErrUnsupported
	}
}

func NewSSE(kind Kind, limits Limits) (SSEDecoder, error) {
	switch kind {
	case Auto:
		return newAutoSSE(limits), nil
	case OpenAIResponses:
		return newOpenAIResponsesSSE(limits), nil
	case OpenAIChatCompletions:
		return newOpenAIChatSSE(limits), nil
	case AnthropicMessages:
		return newAnthropicSSE(limits), nil
	case GoogleGenerateContent:
		return newGoogleSSE(limits), nil
	default:
		return nil, ErrUnsupported
	}
}
