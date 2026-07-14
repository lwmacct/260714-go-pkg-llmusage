package llmusage

import (
	"encoding/json"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/framing"
	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsoncapture"
)

type openAIChatHandler struct {
	options  Options
	json     *jsoncapture.Scanner
	event    *jsoncapture.Scanner
	eventErr error
}

func newOpenAIChatHandler(options Options) *openAIChatHandler {
	return &openAIChatHandler{options: options, json: newScanner(nil, []string{"id", "model", "object", "usage"}, options)}
}

func (h *openAIChatHandler) writeJSON(data []byte) error { return h.json.Write(data) }

func (h *openAIChatHandler) finishJSON(sequence uint64) ([]Result, bool, error) {
	captured, err := h.json.Finish()
	if err != nil {
		return nil, false, err
	}
	recognized := rawString(captured.Fields["object"]) == "chat.completion"
	result, ok, err := resultFromOpenAIChat(captured.Fields, sequence)
	if err != nil || !ok {
		return nil, recognized, err
	}
	return []Result{result}, recognized, nil
}

func (h *openAIChatHandler) writeEventData(data []byte) error {
	if h.event == nil {
		h.event = newScanner(nil, []string{"id", "model", "object", "usage"}, h.options)
	}
	if h.eventErr == nil {
		h.eventErr = h.event.Write(data)
	}
	return nil
}

func (h *openAIChatHandler) finishEvent(event framing.Event) ([]Result, bool, error) {
	scanner := h.event
	eventErr := h.eventErr
	h.event = nil
	h.eventErr = nil
	if scanner == nil {
		return nil, false, nil
	}
	if event.Type != "message" {
		return nil, false, nil
	}
	if eventErr != nil {
		return nil, false, eventErr
	}
	captured, err := scanner.Finish()
	if err != nil {
		return nil, false, err
	}
	recognized := rawString(captured.Fields["object"]) == "chat.completion.chunk"
	result, ok, resultErr := resultFromOpenAIChat(captured.Fields, event.Sequence)
	if resultErr != nil || !ok {
		return nil, recognized, resultErr
	}
	return []Result{result}, recognized, nil
}

func (h *openAIChatHandler) finishStream() ([]Result, error) { return nil, nil }

type openAIChatUsage struct {
	PromptTokens      json.RawMessage `json:"prompt_tokens"`
	CompletionTokens  json.RawMessage `json:"completion_tokens"`
	TotalTokens       json.RawMessage `json:"total_tokens"`
	PromptDetails     json.RawMessage `json:"prompt_tokens_details"`
	CompletionDetails json.RawMessage `json:"completion_tokens_details"`
}

func resultFromOpenAIChat(fields map[string]json.RawMessage, sequence uint64) (Result, bool, error) {
	raw := fields["usage"]
	if isNull(raw) {
		return Result{}, false, nil
	}
	if err := requireObject("usage", raw); err != nil {
		return Result{}, false, err
	}
	var wire openAIChatUsage
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Result{}, false, err
	}
	input, inputPresent, err := parseCounter("prompt_tokens", wire.PromptTokens)
	if err != nil {
		return Result{}, false, err
	}
	output, outputPresent, err := parseCounter("completion_tokens", wire.CompletionTokens)
	if err != nil {
		return Result{}, false, err
	}
	total, totalPresent, err := parseCounter("total_tokens", wire.TotalTokens)
	if err != nil {
		return Result{}, false, err
	}
	usage := Usage{InputTokens: input, OutputTokens: output}
	if !isNull(wire.PromptDetails) {
		var details struct {
			Cached json.RawMessage `json:"cached_tokens"`
		}
		if err := json.Unmarshal(wire.PromptDetails, &details); err != nil {
			return Result{}, false, err
		}
		usage.CachedInputTokens, _, err = parseCounter("prompt_tokens_details.cached_tokens", details.Cached)
		if err != nil {
			return Result{}, false, err
		}
	}
	if !isNull(wire.CompletionDetails) {
		var details struct {
			Reasoning json.RawMessage `json:"reasoning_tokens"`
		}
		if err := json.Unmarshal(wire.CompletionDetails, &details); err != nil {
			return Result{}, false, err
		}
		usage.ReasoningTokens, _, err = parseCounter("completion_tokens_details.reasoning_tokens", details.Reasoning)
		if err != nil {
			return Result{}, false, err
		}
	}
	var totalSource TotalSource
	usage.TotalTokens, totalSource, err = totalFromCounters(input, inputPresent, output, outputPresent, total, totalPresent)
	if err != nil {
		return Result{}, false, err
	}
	return Result{Protocol: ProtocolOpenAIChatCompletions, ResponseID: rawString(fields["id"]), Model: rawString(fields["model"]), Usage: usage, TotalSource: totalSource, RawUsage: append(json.RawMessage(nil), raw...), Sequence: sequence}, true, nil
}
