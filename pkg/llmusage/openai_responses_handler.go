package llmusage

import (
	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/framing"
	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsoncapture"
)

type openAIResponsesHandler struct {
	options  Options
	json     *jsoncapture.Scanner
	event    *jsoncapture.Scanner
	typeName *jsoncapture.Scanner
	eventErr error
	typeErr  error
}

func newOpenAIResponsesHandler(options Options) *openAIResponsesHandler {
	return &openAIResponsesHandler{options: options, json: newScanner(nil, []string{"id", "model", "object", "usage"}, options)}
}

func (h *openAIResponsesHandler) writeJSON(data []byte) error { return h.json.Write(data) }

func (h *openAIResponsesHandler) finishJSON(sequence uint64) ([]Result, bool, error) {
	captured, err := h.json.Finish()
	if err != nil {
		return nil, false, err
	}
	recognized := rawString(captured.Fields["object"]) == "response"
	result, ok, err := resultFromOpenAIResponses(captured.Fields, sequence)
	if err != nil || !ok {
		return nil, recognized, err
	}
	return []Result{result}, recognized, nil
}

func (h *openAIResponsesHandler) writeEventData(data []byte) error {
	if h.event == nil {
		h.event = newScanner([]string{"response"}, []string{"id", "model", "object", "usage"}, h.options)
		h.typeName = newScanner(nil, []string{"type"}, h.options)
	}
	if h.eventErr == nil {
		h.eventErr = h.event.Write(data)
	}
	if h.typeErr == nil {
		h.typeErr = h.typeName.Write(data)
	}
	return nil
}

func (h *openAIResponsesHandler) finishEvent(event framing.Event) ([]Result, bool, error) {
	scanner := h.event
	typeScanner := h.typeName
	eventErr := h.eventErr
	typeErr := h.typeErr
	h.event = nil
	h.typeName = nil
	h.eventErr = nil
	h.typeErr = nil
	if scanner == nil {
		return nil, false, nil
	}
	typeName := rawString(typeScanner.Captured("type"))
	if typeErr == nil {
		var captured jsoncapture.Result
		captured, typeErr = typeScanner.Finish()
		if typeName == "" {
			typeName = rawString(captured.Fields["type"])
		}
	}
	recognized := len(typeName) >= len("response.") && typeName[:len("response.")] == "response."
	if !recognized && len(event.Type) >= len("response.") {
		recognized = event.Type[:len("response.")] == "response."
	}
	if typeErr != nil {
		if recognized || event.Type == "response.completed" {
			return nil, true, typeErr
		}
		return nil, recognized, nil
	}
	if typeName != "response.completed" && event.Type != "response.completed" {
		return nil, recognized, nil
	}
	if eventErr != nil {
		return nil, true, eventErr
	}
	fields, err := scanner.Finish()
	if err != nil {
		return nil, true, err
	}
	result, ok, err := resultFromOpenAIResponses(fields.Fields, event.Sequence)
	if err != nil || !ok {
		return nil, true, err
	}
	return []Result{result}, true, nil
}

func (h *openAIResponsesHandler) finishStream() ([]Result, error) { return nil, nil }
