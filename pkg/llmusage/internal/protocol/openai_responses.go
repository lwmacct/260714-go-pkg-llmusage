package protocol

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsonscan"
)

var openAIResponsesFields = []string{"id", "model", "object", "usage"}

type openAIResponsesJSON struct {
	scanner *jsonscan.Scanner
}

func newOpenAIResponsesJSON(limits Limits) *openAIResponsesJSON {
	return &openAIResponsesJSON{scanner: newScanner(nil, openAIResponsesFields, limits)}
}

func (d *openAIResponsesJSON) Feed(data []byte) error { return d.scanner.Write(data) }

func (d *openAIResponsesJSON) Finish() ([]Result, error) {
	defer d.scanner.Release()
	captured, err := d.scanner.Finish()
	if err != nil {
		return nil, err
	}
	result, ok, err := normalizeOpenAIResponses(captured.Fields, 1)
	if err != nil || !ok {
		return nil, err
	}
	return []Result{result}, nil
}

type openAIResponsesSSE struct {
	limits   Limits
	response *jsonscan.Scanner
	typeName *jsonscan.Scanner
	respErr  error
	typeErr  error
}

func newOpenAIResponsesSSE(limits Limits) *openAIResponsesSSE {
	return &openAIResponsesSSE{limits: limits}
}

func (d *openAIResponsesSSE) FeedEventData(data []byte) error {
	if d.response == nil {
		d.response = newScanner([]string{"response"}, openAIResponsesFields, d.limits)
		d.typeName = newScanner(nil, []string{"type"}, d.limits)
	}
	if d.typeErr == nil {
		d.typeErr = d.typeName.Write(data)
	}
	// Preserve the event signature even when retained response fields exhaust
	// the shared budget later in the same payload.
	if d.respErr == nil {
		d.respErr = d.response.Write(data)
	}
	return nil
}

func (d *openAIResponsesSSE) FinishEvent(event Event) ([]Result, error) {
	response, typeScanner := d.response, d.typeName
	respErr, typeErr := d.respErr, d.typeErr
	d.response, d.typeName, d.respErr, d.typeErr = nil, nil, nil, nil
	if response == nil {
		return nil, nil
	}
	defer response.Release()
	defer typeScanner.Release()
	typeName := rawString(typeScanner.Captured("type"))
	if typeErr == nil {
		captured, err := typeScanner.Finish()
		typeErr = err
		if typeName == "" {
			typeName = rawString(captured.Fields["type"])
		}
	}
	recognized := strings.HasPrefix(typeName, "response.") || strings.HasPrefix(event.Type, "response.")
	if typeErr != nil {
		if recognized {
			return nil, typeErr
		}
		return nil, nil
	}
	if typeName != "response.completed" && event.Type != "response.completed" {
		return nil, nil
	}
	if respErr != nil {
		return nil, respErr
	}
	fields, err := response.Finish()
	if err != nil {
		return nil, err
	}
	result, ok, err := normalizeOpenAIResponses(fields.Fields, event.Sequence)
	if err != nil || !ok {
		return nil, err
	}
	return []Result{result}, nil
}

func (d *openAIResponsesSSE) FinishStream() ([]Result, error) { return nil, nil }

type openAIResponsesUsage struct {
	InputTokens   json.RawMessage `json:"input_tokens"`
	InputDetails  json.RawMessage `json:"input_tokens_details"`
	OutputTokens  json.RawMessage `json:"output_tokens"`
	OutputDetails json.RawMessage `json:"output_tokens_details"`
	TotalTokens   json.RawMessage `json:"total_tokens"`
}

func normalizeOpenAIResponses(fields map[string]json.RawMessage, sequence uint64) (Result, bool, error) {
	raw := fields["usage"]
	if isNull(raw) {
		return Result{}, false, nil
	}
	if err := requireObject("usage", raw); err != nil {
		return Result{}, false, err
	}
	var wire openAIResponsesUsage
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Result{}, false, err
	}
	input, inputPresent, err := parseCounter("input_tokens", wire.InputTokens)
	if err != nil {
		return Result{}, false, err
	}
	output, outputPresent, err := parseCounter("output_tokens", wire.OutputTokens)
	if err != nil {
		return Result{}, false, err
	}
	total, totalPresent, err := parseCounter("total_tokens", wire.TotalTokens)
	if err != nil {
		return Result{}, false, err
	}
	usage := Usage{InputTokens: input, OutputTokens: output}
	if !isNull(wire.InputDetails) {
		var details struct {
			Cached json.RawMessage `json:"cached_tokens"`
			Write  json.RawMessage `json:"cache_write_tokens"`
		}
		if err := json.Unmarshal(wire.InputDetails, &details); err != nil {
			return Result{}, false, err
		}
		usage.CachedInputTokens, _, err = parseCounter("input_tokens_details.cached_tokens", details.Cached)
		if err != nil {
			return Result{}, false, err
		}
		usage.CacheWriteTokens, _, err = parseCounter("input_tokens_details.cache_write_tokens", details.Write)
		if err != nil {
			return Result{}, false, err
		}
	}
	if !isNull(wire.OutputDetails) {
		var details struct {
			Reasoning json.RawMessage `json:"reasoning_tokens"`
		}
		if err := json.Unmarshal(wire.OutputDetails, &details); err != nil {
			return Result{}, false, err
		}
		usage.ReasoningTokens, _, err = parseCounter("output_tokens_details.reasoning_tokens", details.Reasoning)
		if err != nil {
			return Result{}, false, err
		}
	}
	var source TotalSource
	usage.TotalTokens, source, err = totalFromCounters(input, inputPresent, output, outputPresent, total, totalPresent)
	if err != nil {
		return Result{}, false, fmt.Errorf("openai responses: %w", err)
	}
	return Result{Protocol: OpenAIResponses, ResponseID: rawString(fields["id"]), Model: rawString(fields["model"]), Usage: usage, TotalSource: source, RawUsage: append(json.RawMessage(nil), raw...), Sequence: sequence}, true, nil
}
