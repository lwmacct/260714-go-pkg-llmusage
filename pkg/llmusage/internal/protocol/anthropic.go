package protocol

import (
	"encoding/json"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsonscan"
)

type anthropicJSON struct{ scanner *jsonscan.Scanner }

func newAnthropicJSON(limits Limits) *anthropicJSON {
	return &anthropicJSON{scanner: newScanner(nil, []string{"type", "id", "model", "usage"}, limits)}
}
func (d *anthropicJSON) Feed(data []byte) error { return d.scanner.Write(data) }
func (d *anthropicJSON) Finish() ([]Result, error) {
	captured, err := d.scanner.Finish()
	if err != nil {
		return nil, err
	}
	result, ok, err := normalizeAnthropic(captured.Fields["id"], captured.Fields["model"], captured.Fields["usage"], 1)
	if err != nil || !ok {
		return nil, err
	}
	return []Result{result}, nil
}

type anthropicSSE struct {
	limits     Limits
	event      *jsonscan.Scanner
	message    *jsonscan.Scanner
	eventErr   error
	messageErr error
	id         string
	model      string
	usage      map[string]json.RawMessage
	sequence   uint64
	recognized bool
	emitted    bool
}

func newAnthropicSSE(limits Limits) *anthropicSSE { return &anthropicSSE{limits: limits} }
func (d *anthropicSSE) FeedEventData(data []byte) error {
	if d.event == nil {
		d.event = newScanner(nil, []string{"type", "usage"}, d.limits)
		d.message = newScanner([]string{"message"}, []string{"id", "model", "usage"}, d.limits)
	}
	if d.eventErr == nil {
		d.eventErr = d.event.Write(data)
	}
	if d.messageErr == nil {
		d.messageErr = d.message.Write(data)
	}
	return nil
}
func (d *anthropicSSE) FinishEvent(event Event) ([]Result, error) {
	scanner, message := d.event, d.message
	eventErr, messageErr := d.eventErr, d.messageErr
	d.event, d.message, d.eventErr, d.messageErr = nil, nil, nil, nil
	if scanner == nil {
		return nil, nil
	}
	typeName := rawString(scanner.Captured("type"))
	var captured jsonscan.Result
	if eventErr == nil {
		captured, eventErr = scanner.Finish()
		if typeName == "" {
			typeName = rawString(captured.Fields["type"])
		}
	}
	if typeName == "" {
		typeName = event.Type
	}
	if !isStrongAnthropicEvent(typeName) {
		return nil, nil
	}
	d.recognized = true
	if eventErr != nil {
		return nil, eventErr
	}
	switch typeName {
	case "message_start":
		if messageErr != nil {
			return nil, messageErr
		}
		fields, err := message.Finish()
		if err != nil {
			return nil, err
		}
		d.id = rawString(fields.Fields["id"])
		d.model = rawString(fields.Fields["model"])
		if err := d.mergeUsage(fields.Fields["usage"]); err != nil {
			return nil, err
		}
	case "message_delta":
		if err := d.mergeUsage(captured.Fields["usage"]); err != nil {
			return nil, err
		}
		d.sequence = event.Sequence
	case "message_stop":
		if d.emitted || d.usage == nil {
			return nil, nil
		}
		result, err := d.result(event.Sequence)
		if err != nil {
			return nil, err
		}
		d.emitted = true
		return []Result{result}, nil
	}
	return nil, nil
}
func (d *anthropicSSE) FinishStream() ([]Result, error) {
	if !d.recognized || d.emitted || d.usage == nil {
		return nil, nil
	}
	result, err := d.result(d.sequence)
	if err != nil {
		return nil, err
	}
	d.emitted = true
	return []Result{result}, nil
}
func (d *anthropicSSE) mergeUsage(raw json.RawMessage) error {
	if isNull(raw) {
		return nil
	}
	if err := requireObject("usage", raw); err != nil {
		return err
	}
	var update map[string]json.RawMessage
	if err := json.Unmarshal(raw, &update); err != nil {
		return err
	}
	if d.usage == nil {
		d.usage = make(map[string]json.RawMessage)
	}
	for key, value := range update {
		d.usage[key] = append(json.RawMessage(nil), value...)
	}
	merged, err := json.Marshal(d.usage)
	if err != nil {
		return err
	}
	return resultLimit(d.id, d.model, merged, d.limits)
}
func (d *anthropicSSE) result(sequence uint64) (Result, error) {
	raw, err := json.Marshal(d.usage)
	if err != nil {
		return Result{}, err
	}
	rawID, _ := json.Marshal(d.id)
	rawModel, _ := json.Marshal(d.model)
	result, _, err := normalizeAnthropic(rawID, rawModel, raw, sequence)
	return result, err
}

func isStrongAnthropicEvent(name string) bool {
	switch name {
	case "message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop":
		return true
	default:
		return false
	}
}

func normalizeAnthropic(rawID, rawModel, raw json.RawMessage, sequence uint64) (Result, bool, error) {
	if isNull(raw) {
		return Result{}, false, nil
	}
	if err := requireObject("usage", raw); err != nil {
		return Result{}, false, err
	}
	var wire struct {
		Input       json.RawMessage `json:"input_tokens"`
		Output      json.RawMessage `json:"output_tokens"`
		CacheRead   json.RawMessage `json:"cache_read_input_tokens"`
		CacheCreate json.RawMessage `json:"cache_creation_input_tokens"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Result{}, false, err
	}
	input, _, err := parseCounter("input_tokens", wire.Input)
	if err != nil {
		return Result{}, false, err
	}
	output, _, err := parseCounter("output_tokens", wire.Output)
	if err != nil {
		return Result{}, false, err
	}
	cached, _, err := parseCounter("cache_read_input_tokens", wire.CacheRead)
	if err != nil {
		return Result{}, false, err
	}
	created, _, err := parseCounter("cache_creation_input_tokens", wire.CacheCreate)
	if err != nil {
		return Result{}, false, err
	}
	return Result{Protocol: AnthropicMessages, ResponseID: rawString(rawID), Model: rawString(rawModel), Usage: Usage{InputTokens: input, OutputTokens: output, CachedInputTokens: cached, CacheWriteTokens: created}, TotalSource: TotalUnknown, RawUsage: append(json.RawMessage(nil), raw...), Sequence: sequence}, true, nil
}
