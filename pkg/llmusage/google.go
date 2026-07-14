package llmusage

import (
	"encoding/json"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/framing"
	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsoncapture"
)

type googleHandler struct {
	options     Options
	json        *jsoncapture.Scanner
	event       *jsoncapture.Scanner
	eventErr    error
	snapshot    Result
	hasSnapshot bool
	recognized  bool
	id          string
	model       string
}

func newGoogleHandler(options Options) *googleHandler {
	fields := []string{"responseId", "modelVersion", "usageMetadata"}
	return &googleHandler{options: options, json: newScanner(nil, fields, options)}
}

func (h *googleHandler) writeJSON(data []byte) error { return h.json.Write(data) }

func (h *googleHandler) finishJSON(sequence uint64) ([]Result, bool, error) {
	captured, err := h.json.Finish()
	if err != nil {
		return nil, false, err
	}
	_, recognized := captured.Fields["usageMetadata"]
	result, ok, err := resultFromGoogle(captured.Fields, sequence)
	if err != nil || !ok {
		return nil, recognized, err
	}
	return []Result{result}, recognized, nil
}

func (h *googleHandler) writeEventData(data []byte) error {
	if h.event == nil {
		h.event = newScanner(nil, []string{"responseId", "modelVersion", "usageMetadata"}, h.options)
	}
	if h.eventErr == nil {
		h.eventErr = h.event.Write(data)
	}
	return nil
}

func (h *googleHandler) finishEvent(event framing.Event) ([]Result, bool, error) {
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
	_, recognized := captured.Fields["usageMetadata"]
	if !recognized {
		return nil, false, nil
	}
	h.recognized = true
	result, ok, err := resultFromGoogle(captured.Fields, event.Sequence)
	if err != nil || !ok {
		return nil, true, err
	}
	if result.ResponseID != "" {
		h.id = result.ResponseID
	} else {
		result.ResponseID = h.id
	}
	if result.Model != "" {
		h.model = result.Model
	} else {
		result.Model = h.model
	}
	h.snapshot = result
	h.hasSnapshot = true
	return nil, true, nil
}

func (h *googleHandler) finishStream() ([]Result, error) {
	if !h.hasSnapshot {
		return nil, nil
	}
	return []Result{h.snapshot}, nil
}

func resultFromGoogle(fields map[string]json.RawMessage, sequence uint64) (Result, bool, error) {
	raw := fields["usageMetadata"]
	if isNull(raw) {
		return Result{}, false, nil
	}
	if err := requireObject("usageMetadata", raw); err != nil {
		return Result{}, false, err
	}
	var wire struct {
		Input    json.RawMessage `json:"promptTokenCount"`
		Output   json.RawMessage `json:"candidatesTokenCount"`
		Total    json.RawMessage `json:"totalTokenCount"`
		Cached   json.RawMessage `json:"cachedContentTokenCount"`
		Thoughts json.RawMessage `json:"thoughtsTokenCount"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Result{}, false, err
	}
	input, _, err := parseCounter("promptTokenCount", wire.Input)
	if err != nil {
		return Result{}, false, err
	}
	output, _, err := parseCounter("candidatesTokenCount", wire.Output)
	if err != nil {
		return Result{}, false, err
	}
	total, totalPresent, err := parseCounter("totalTokenCount", wire.Total)
	if err != nil {
		return Result{}, false, err
	}
	cached, _, err := parseCounter("cachedContentTokenCount", wire.Cached)
	if err != nil {
		return Result{}, false, err
	}
	thoughts, _, err := parseCounter("thoughtsTokenCount", wire.Thoughts)
	if err != nil {
		return Result{}, false, err
	}
	source := TotalUnknown
	if totalPresent {
		source = TotalReported
	}
	return Result{Protocol: ProtocolGoogleGenerateContent, ResponseID: rawString(fields["responseId"]), Model: rawString(fields["modelVersion"]), Usage: Usage{InputTokens: input, OutputTokens: output, TotalTokens: total, CachedInputTokens: cached, ReasoningTokens: thoughts}, TotalSource: source, RawUsage: append(json.RawMessage(nil), raw...), Sequence: sequence}, true, nil
}
