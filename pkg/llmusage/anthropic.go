package llmusage

import (
	"encoding/json"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/framing"
	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsoncapture"
)

type anthropicHandler struct {
	options    Options
	json       *jsoncapture.Scanner
	event      *jsoncapture.Scanner
	message    *jsoncapture.Scanner
	eventErr   error
	messageErr error
	id         string
	model      string
	usage      map[string]json.RawMessage
	sequence   uint64
	recognized bool
	emitted    bool
}

func newAnthropicHandler(options Options) *anthropicHandler {
	return &anthropicHandler{options: options, json: newScanner(nil, []string{"type", "id", "model", "usage"}, options)}
}

func (h *anthropicHandler) writeJSON(data []byte) error { return h.json.Write(data) }

func (h *anthropicHandler) finishJSON(sequence uint64) ([]Result, bool, error) {
	captured, err := h.json.Finish()
	if err != nil {
		return nil, false, err
	}
	recognized := rawString(captured.Fields["type"]) == "message"
	result, ok, err := resultFromAnthropic(captured.Fields["id"], captured.Fields["model"], captured.Fields["usage"], sequence)
	if err != nil || !ok {
		return nil, recognized, err
	}
	return []Result{result}, recognized, nil
}

func (h *anthropicHandler) writeEventData(data []byte) error {
	if h.event == nil {
		h.event = newScanner(nil, []string{"type", "usage"}, h.options)
		h.message = newScanner([]string{"message"}, []string{"id", "model", "usage"}, h.options)
	}
	if h.eventErr == nil {
		h.eventErr = h.event.Write(data)
	}
	if h.messageErr == nil {
		h.messageErr = h.message.Write(data)
	}
	return nil
}

func (h *anthropicHandler) finishEvent(event framing.Event) ([]Result, bool, error) {
	scanner := h.event
	messageScanner := h.message
	eventErr := h.eventErr
	messageErr := h.messageErr
	h.event = nil
	h.message = nil
	h.eventErr = nil
	h.messageErr = nil
	if scanner == nil {
		return nil, false, nil
	}
	var captured jsoncapture.Result
	typeName := rawString(scanner.Captured("type"))
	if eventErr == nil {
		captured, eventErr = scanner.Finish()
	}
	if typeName == "" {
		typeName = rawString(captured.Fields["type"])
	}
	if typeName == "" {
		typeName = event.Type
	}
	recognized := isAnthropicEvent(typeName)
	if eventErr != nil {
		if !recognized {
			return nil, false, nil
		}
		return nil, recognized, eventErr
	}
	if !recognized {
		return nil, false, nil
	}
	h.recognized = true
	switch typeName {
	case "message_start":
		if messageErr != nil {
			return nil, true, messageErr
		}
		fields, err := messageScanner.Finish()
		if err != nil {
			return nil, true, err
		}
		h.id = rawString(fields.Fields["id"])
		h.model = rawString(fields.Fields["model"])
		if err := h.mergeUsage(fields.Fields["usage"]); err != nil {
			return nil, true, err
		}
	case "message_delta":
		if err := h.mergeUsage(captured.Fields["usage"]); err != nil {
			return nil, true, err
		}
		h.sequence = event.Sequence
	case "message_stop":
		if h.emitted || h.usage == nil {
			return nil, true, nil
		}
		result, err := h.result(event.Sequence)
		if err != nil {
			return nil, true, err
		}
		h.emitted = true
		return []Result{result}, true, nil
	}
	return nil, true, nil
}

func (h *anthropicHandler) finishStream() ([]Result, error) {
	if !h.recognized || h.emitted || h.usage == nil {
		return nil, nil
	}
	result, err := h.result(h.sequence)
	if err != nil {
		return nil, err
	}
	h.emitted = true
	return []Result{result}, nil
}

func (h *anthropicHandler) mergeUsage(raw json.RawMessage) error {
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
	if h.usage == nil {
		h.usage = make(map[string]json.RawMessage)
	}
	for key, value := range update {
		h.usage[key] = append(json.RawMessage(nil), value...)
	}
	merged, err := json.Marshal(h.usage)
	if err != nil {
		return err
	}
	if len(h.id)+len(h.model)+len(merged) > h.options.MaxResultBytes {
		return jsoncapture.ErrLimit
	}
	return nil
}

func (h *anthropicHandler) result(sequence uint64) (Result, error) {
	raw, err := json.Marshal(h.usage)
	if err != nil {
		return Result{}, err
	}
	rawID, _ := json.Marshal(h.id)
	rawModel, _ := json.Marshal(h.model)
	result, _, err := resultFromAnthropic(rawID, rawModel, raw, sequence)
	return result, err
}

func isAnthropicEvent(name string) bool {
	switch name {
	case "message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop", "ping", "error":
		return true
	default:
		return false
	}
}

func resultFromAnthropic(rawID, rawModel, raw json.RawMessage, sequence uint64) (Result, bool, error) {
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
	cacheWrite, _, err := parseCounter("cache_creation_input_tokens", wire.CacheCreate)
	if err != nil {
		return Result{}, false, err
	}
	return Result{Protocol: ProtocolAnthropicMessages, ResponseID: rawString(rawID), Model: rawString(rawModel), Usage: Usage{InputTokens: input, OutputTokens: output, CachedInputTokens: cached, CacheWriteTokens: cacheWrite}, TotalSource: TotalUnknown, RawUsage: append(json.RawMessage(nil), raw...), Sequence: sequence}, true, nil
}
