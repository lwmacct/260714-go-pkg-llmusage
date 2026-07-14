package protocol

import (
	"encoding/json"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsonscan"
)

var googleFields = []string{"responseId", "modelVersion", "usageMetadata"}

type googleJSON struct{ scanner *jsonscan.Scanner }

func newGoogleJSON(limits Limits) *googleJSON {
	return &googleJSON{scanner: newScanner(nil, googleFields, limits)}
}
func (d *googleJSON) Feed(data []byte) error { return d.scanner.Write(data) }
func (d *googleJSON) Finish() ([]Result, error) {
	defer d.scanner.Release()
	captured, err := d.scanner.Finish()
	if err != nil {
		return nil, err
	}
	if _, ok := captured.Fields["usageMetadata"]; !ok {
		return nil, ErrUnsupported
	}
	result, ok, err := normalizeGoogle(captured.Fields, 1)
	if err != nil || !ok {
		return nil, err
	}
	return []Result{result}, nil
}

type googleSSE struct {
	limits      Limits
	event       *jsonscan.Scanner
	eventErr    error
	snapshot    Result
	hasSnapshot bool
	id          string
	model       string
}

func newGoogleSSE(limits Limits) *googleSSE { return &googleSSE{limits: limits} }
func (d *googleSSE) FeedEventData(data []byte) error {
	if d.event == nil {
		d.event = newScanner(nil, googleFields, d.limits)
	}
	if d.eventErr == nil {
		d.eventErr = d.event.Write(data)
	}
	return nil
}
func (d *googleSSE) FinishEvent(event Event) ([]Result, error) {
	scanner, eventErr := d.event, d.eventErr
	d.event, d.eventErr = nil, nil
	if scanner == nil || event.Type != "message" {
		if scanner != nil {
			scanner.Release()
		}
		return nil, nil
	}
	defer scanner.Release()
	if eventErr != nil {
		if len(scanner.Captured("usageMetadata")) > 0 {
			return nil, eventErr
		}
		return nil, nil
	}
	captured, err := scanner.Finish()
	if err != nil {
		return nil, err
	}
	if _, ok := captured.Fields["usageMetadata"]; !ok {
		return nil, nil
	}
	result, ok, err := normalizeGoogle(captured.Fields, event.Sequence)
	if err != nil || !ok {
		return nil, err
	}
	if result.ResponseID != "" {
		d.id = result.ResponseID
	} else {
		result.ResponseID = d.id
	}
	if result.Model != "" {
		d.model = result.Model
	} else {
		result.Model = d.model
	}
	d.snapshot, d.hasSnapshot = result, true
	return nil, nil
}
func (d *googleSSE) FinishStream() ([]Result, error) {
	if !d.hasSnapshot {
		return nil, nil
	}
	return []Result{d.snapshot}, nil
}

func normalizeGoogle(fields map[string]json.RawMessage, sequence uint64) (Result, bool, error) {
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
	return Result{Protocol: GoogleGenerateContent, ResponseID: rawString(fields["responseId"]), Model: rawString(fields["modelVersion"]), Usage: Usage{InputTokens: input, OutputTokens: output, TotalTokens: total, CachedInputTokens: cached, ReasoningTokens: thoughts}, TotalSource: source, RawUsage: append(json.RawMessage(nil), raw...), Sequence: sequence}, true, nil
}
