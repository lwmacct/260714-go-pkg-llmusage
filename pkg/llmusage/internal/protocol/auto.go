package protocol

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsonscan"
)

var autoRootFields = []string{"type", "object", "id", "model", "usage", "responseId", "modelVersion", "usageMetadata"}

type autoJSON struct{ scanner *jsonscan.Scanner }

func newAutoJSON(limits Limits) *autoJSON {
	return &autoJSON{scanner: newScanner(nil, autoRootFields, limits)}
}
func (d *autoJSON) Feed(data []byte) error { return d.scanner.Write(data) }
func (d *autoJSON) Finish() ([]Result, error) {
	defer d.scanner.Release()
	captured, err := d.scanner.Finish()
	if err != nil {
		return nil, err
	}
	fields := captured.Fields
	kind, err := detectJSON(fields)
	if err != nil {
		return nil, err
	}
	var result Result
	var ok bool
	switch kind {
	case OpenAIResponses:
		result, ok, err = normalizeOpenAIResponses(fields, 1)
	case OpenAIChatCompletions:
		result, ok, err = normalizeOpenAIChat(fields, 1)
	case AnthropicMessages:
		result, ok, err = normalizeAnthropic(fields["id"], fields["model"], fields["usage"], 1)
	case GoogleGenerateContent:
		result, ok, err = normalizeGoogle(fields, 1)
	}
	if err != nil || !ok {
		return nil, err
	}
	return []Result{result}, nil
}

func detectJSON(fields map[string]json.RawMessage) (Kind, error) {
	var matches []Kind
	switch rawString(fields["object"]) {
	case "response":
		matches = append(matches, OpenAIResponses)
	case "chat.completion":
		matches = append(matches, OpenAIChatCompletions)
	}
	if rawString(fields["type"]) == "message" {
		matches = append(matches, AnthropicMessages)
	}
	if _, ok := fields["usageMetadata"]; ok {
		matches = append(matches, GoogleGenerateContent)
	}
	if len(matches) != 1 {
		return "", ErrUnsupported
	}
	return matches[0], nil
}

type autoSSE struct {
	limits   Limits
	selected SSEDecoder
	root     *jsonscan.Scanner
	response *jsonscan.Scanner
	message  *jsonscan.Scanner
	rootErr  error
	respErr  error
	msgErr   error
}

func newAutoSSE(limits Limits) *autoSSE { return &autoSSE{limits: limits} }

func (d *autoSSE) FeedEventData(data []byte) error {
	if d.selected != nil {
		return d.selected.FeedEventData(data)
	}
	if d.root == nil {
		d.root = newScanner(nil, autoRootFields, d.limits)
		d.response = newScanner([]string{"response"}, openAIResponsesFields, d.limits)
		d.message = newScanner([]string{"message"}, []string{"id", "model", "usage"}, d.limits)
	}
	if d.rootErr == nil {
		d.rootErr = d.root.Write(data)
	}
	if d.respErr == nil {
		d.respErr = d.response.Write(data)
	}
	if d.msgErr == nil {
		d.msgErr = d.message.Write(data)
	}
	return nil
}

func (d *autoSSE) FinishEvent(event Event) ([]Result, error) {
	if d.selected != nil {
		return d.selected.FinishEvent(event)
	}
	root, response, message := d.root, d.response, d.message
	rootErr, respErr, msgErr := d.rootErr, d.respErr, d.msgErr
	d.root, d.response, d.message = nil, nil, nil
	d.rootErr, d.respErr, d.msgErr = nil, nil, nil
	if root == nil {
		return nil, nil
	}
	defer root.Release()
	defer response.Release()
	defer message.Release()

	fields := rootFields(root, rootErr)
	typeName := rawString(fields["type"])
	if typeName == "" {
		typeName = event.Type
	}
	kind, matched := detectSSE(typeName, fields)
	if !matched {
		return nil, nil
	}
	if kind == Auto {
		return nil, ErrUnsupported
	}
	if rootErr != nil && kind != OpenAIResponses {
		return nil, rootErr
	}

	selected, err := NewSSE(kind, d.limits)
	if err != nil {
		return nil, err
	}
	d.selected = selected
	switch decoder := selected.(type) {
	case *openAIResponsesSSE:
		if respErr != nil {
			return nil, respErr
		}
		captured, err := response.Finish()
		if err != nil {
			return nil, err
		}
		if typeName != "response.completed" {
			return nil, nil
		}
		result, ok, err := normalizeOpenAIResponses(captured.Fields, event.Sequence)
		if err != nil || !ok {
			return nil, err
		}
		return []Result{result}, nil
	case *openAIChatSSE:
		result, ok, err := normalizeOpenAIChat(fields, event.Sequence)
		if err != nil || !ok {
			return nil, err
		}
		return []Result{result}, nil
	case *anthropicSSE:
		return bootstrapAnthropic(decoder, typeName, fields, message, msgErr, event.Sequence)
	case *googleSSE:
		result, ok, err := normalizeGoogle(fields, event.Sequence)
		if err != nil || !ok {
			return nil, err
		}
		decoder.id, decoder.model = result.ResponseID, result.Model
		decoder.snapshot, decoder.hasSnapshot = result, true
		return nil, nil
	default:
		return nil, errors.New("invalid auto decoder")
	}
}

func (d *autoSSE) FinishStream() ([]Result, error) {
	if d.selected == nil {
		return nil, ErrUnsupported
	}
	return d.selected.FinishStream()
}

func rootFields(scanner *jsonscan.Scanner, scanErr error) map[string]json.RawMessage {
	fields := make(map[string]json.RawMessage)
	for _, name := range autoRootFields {
		if raw := scanner.Captured(name); len(raw) > 0 {
			fields[name] = raw
		}
	}
	if scanErr == nil {
		if captured, err := scanner.Finish(); err == nil {
			for name, raw := range captured.Fields {
				fields[name] = raw
			}
		}
	}
	return fields
}

func detectSSE(typeName string, fields map[string]json.RawMessage) (Kind, bool) {
	var matches []Kind
	if strings.HasPrefix(typeName, "response.") {
		matches = append(matches, OpenAIResponses)
	}
	if rawString(fields["object"]) == "chat.completion.chunk" {
		matches = append(matches, OpenAIChatCompletions)
	}
	if isStrongAnthropicEvent(typeName) {
		matches = append(matches, AnthropicMessages)
	}
	if _, ok := fields["usageMetadata"]; ok {
		matches = append(matches, GoogleGenerateContent)
	}
	if len(matches) == 0 {
		return "", false
	}
	if len(matches) > 1 {
		return Auto, true
	}
	return matches[0], true
}

func bootstrapAnthropic(decoder *anthropicSSE, typeName string, fields map[string]json.RawMessage, message *jsonscan.Scanner, messageErr error, sequence uint64) ([]Result, error) {
	decoder.recognized = true
	switch typeName {
	case "message_start":
		if messageErr != nil {
			return nil, messageErr
		}
		captured, err := message.Finish()
		if err != nil {
			return nil, err
		}
		decoder.id = rawString(captured.Fields["id"])
		decoder.model = rawString(captured.Fields["model"])
		if err := decoder.mergeUsage(captured.Fields["usage"]); err != nil {
			return nil, err
		}
	case "message_delta":
		if err := decoder.mergeUsage(fields["usage"]); err != nil {
			return nil, err
		}
		decoder.sequence = sequence
	}
	return nil, nil
}
