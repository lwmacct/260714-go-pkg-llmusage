package llmusage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

type openAIResponsesUsage struct {
	InputTokens   json.RawMessage `json:"input_tokens"`
	InputDetails  json.RawMessage `json:"input_tokens_details"`
	OutputTokens  json.RawMessage `json:"output_tokens"`
	OutputDetails json.RawMessage `json:"output_tokens_details"`
	TotalTokens   json.RawMessage `json:"total_tokens"`
}

type openAIInputDetails struct {
	CachedTokens     json.RawMessage `json:"cached_tokens"`
	CacheWriteTokens json.RawMessage `json:"cache_write_tokens"`
}

type openAIOutputDetails struct {
	ReasoningTokens json.RawMessage `json:"reasoning_tokens"`
}

func resultFromFields(protocol Protocol, fields map[string]json.RawMessage, sequence uint64) (Result, bool, error) {
	rawUsage, exists := fields["usage"]
	if !exists || bytes.Equal(bytes.TrimSpace(rawUsage), []byte("null")) {
		return Result{}, false, nil
	}
	if len(rawUsage) == 0 || rawUsage[0] != '{' {
		return Result{}, false, fmt.Errorf("usage must be an object")
	}
	var wire openAIResponsesUsage
	if err := json.Unmarshal(rawUsage, &wire); err != nil {
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

	usage := Usage{InputTokens: input, OutputTokens: output, TotalTokens: total}
	if len(wire.InputDetails) > 0 && !bytes.Equal(bytes.TrimSpace(wire.InputDetails), []byte("null")) {
		var details openAIInputDetails
		if err := json.Unmarshal(wire.InputDetails, &details); err != nil {
			return Result{}, false, err
		}
		usage.CachedInputTokens, _, err = parseCounter("input_tokens_details.cached_tokens", details.CachedTokens)
		if err != nil {
			return Result{}, false, err
		}
		usage.CacheWriteTokens, _, err = parseCounter("input_tokens_details.cache_write_tokens", details.CacheWriteTokens)
		if err != nil {
			return Result{}, false, err
		}
	}
	if len(wire.OutputDetails) > 0 && !bytes.Equal(bytes.TrimSpace(wire.OutputDetails), []byte("null")) {
		var details openAIOutputDetails
		if err := json.Unmarshal(wire.OutputDetails, &details); err != nil {
			return Result{}, false, err
		}
		usage.ReasoningTokens, _, err = parseCounter("output_tokens_details.reasoning_tokens", details.ReasoningTokens)
		if err != nil {
			return Result{}, false, err
		}
	}
	totalSource := TotalReported
	if !totalPresent {
		if inputPresent && outputPresent {
			if input > int64(^uint64(0)>>1)-output {
				return Result{}, false, fmt.Errorf("derived total_tokens overflows int64")
			}
			usage.TotalTokens = input + output
			totalSource = TotalDerived
		} else {
			totalSource = TotalUnknown
		}
	}
	return Result{
		Protocol:    protocol,
		ResponseID:  rawString(fields["id"]),
		Model:       rawString(fields["model"]),
		Usage:       usage,
		TotalSource: totalSource,
		RawUsage:    append(json.RawMessage(nil), rawUsage...),
		Sequence:    sequence,
	}, true, nil
}

func parseCounter(name string, raw json.RawMessage) (int64, bool, error) {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return 0, false, nil
	}
	parsed, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil || parsed < 0 {
		return 0, true, fmt.Errorf("invalid %s", name)
	}
	return parsed, true, nil
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}
