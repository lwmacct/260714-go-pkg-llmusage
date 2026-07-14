package protocol

import (
	"encoding/json"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsonscan"
)

var openAIChatFields = []string{"id", "model", "object", "usage"}

type openAIChatJSON struct{ scanner *jsonscan.Scanner }

func newOpenAIChatJSON(limits Limits) *openAIChatJSON {
	return &openAIChatJSON{scanner: newScanner(nil, openAIChatFields, limits)}
}
func (d *openAIChatJSON) Feed(data []byte) error { return d.scanner.Write(data) }
func (d *openAIChatJSON) Finish() ([]Result, error) {
	defer d.scanner.Release()
	captured, err := d.scanner.Finish()
	if err != nil {
		return nil, err
	}
	result, ok, err := normalizeOpenAIChat(captured.Fields, 1)
	if err != nil || !ok {
		return nil, err
	}
	return []Result{result}, nil
}

type openAIChatSSE struct {
	limits      Limits
	event       *jsonscan.Scanner
	err         error
	prefix      [len("[DONE]")]byte
	prefixLen   int
	passthrough bool
}

func newOpenAIChatSSE(limits Limits) *openAIChatSSE { return &openAIChatSSE{limits: limits} }
func (d *openAIChatSSE) FeedEventData(data []byte) error {
	if d.passthrough {
		return d.feedJSON(data)
	}
	needed := len(d.prefix) - d.prefixLen
	consumed := min(needed, len(data))
	copy(d.prefix[d.prefixLen:], data[:consumed])
	d.prefixLen += consumed
	data = data[consumed:]
	if string(d.prefix[:d.prefixLen]) != "[DONE]"[:d.prefixLen] || len(data) > 0 {
		d.passthrough = true
		if err := d.feedJSON(d.prefix[:d.prefixLen]); err != nil {
			return err
		}
		return d.feedJSON(data)
	}
	return nil
}
func (d *openAIChatSSE) feedJSON(data []byte) error {
	if d.event == nil {
		d.event = newScanner(nil, openAIChatFields, d.limits)
	}
	if d.err == nil {
		d.err = d.event.Write(data)
	}
	return nil
}
func (d *openAIChatSSE) FinishEvent(event Event) ([]Result, error) {
	if !d.passthrough && d.prefixLen == len("[DONE]") && string(d.prefix[:]) == "[DONE]" {
		d.resetPrefix()
		return nil, nil
	}
	if !d.passthrough && d.prefixLen > 0 {
		if err := d.feedJSON(d.prefix[:d.prefixLen]); err != nil {
			return nil, err
		}
	}
	d.resetPrefix()
	scanner, eventErr := d.event, d.err
	d.event, d.err = nil, nil
	if scanner == nil {
		return nil, nil
	}
	defer scanner.Release()
	if event.Type != "message" {
		return nil, nil
	}
	if eventErr != nil {
		if string(scanner.Captured("object")) == `"chat.completion.chunk"` {
			return nil, eventErr
		}
		return nil, nil
	}
	captured, err := scanner.Finish()
	if err != nil {
		return nil, err
	}
	if rawString(captured.Fields["object"]) != "chat.completion.chunk" {
		return nil, nil
	}
	result, ok, err := normalizeOpenAIChat(captured.Fields, event.Sequence)
	if err != nil || !ok {
		return nil, err
	}
	return []Result{result}, nil
}
func (d *openAIChatSSE) FinishStream() ([]Result, error) { return nil, nil }
func (d *openAIChatSSE) resetPrefix() {
	d.prefixLen = 0
	d.passthrough = false
}

type openAIChatUsage struct {
	PromptTokens      json.RawMessage `json:"prompt_tokens"`
	CompletionTokens  json.RawMessage `json:"completion_tokens"`
	TotalTokens       json.RawMessage `json:"total_tokens"`
	PromptDetails     json.RawMessage `json:"prompt_tokens_details"`
	CompletionDetails json.RawMessage `json:"completion_tokens_details"`
}

func normalizeOpenAIChat(fields map[string]json.RawMessage, sequence uint64) (Result, bool, error) {
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
	var source TotalSource
	usage.TotalTokens, source, err = totalFromCounters(input, inputPresent, output, outputPresent, total, totalPresent)
	if err != nil {
		return Result{}, false, err
	}
	return Result{Protocol: OpenAIChatCompletions, ResponseID: rawString(fields["id"]), Model: rawString(fields["model"]), Usage: usage, TotalSource: source, RawUsage: append(json.RawMessage(nil), raw...), Sequence: sequence}, true, nil
}
