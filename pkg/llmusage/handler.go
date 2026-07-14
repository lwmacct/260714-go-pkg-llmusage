package llmusage

import "github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/framing"

func newProtocolHandler(options Options) protocolHandler {
	if options.Protocol == ProtocolAuto {
		return newAutoHandler(options)
	}
	return newExplicitHandler(options.Protocol, options)
}

func newExplicitHandler(protocol Protocol, options Options) protocolHandler {
	switch protocol {
	case ProtocolOpenAIResponses:
		return newOpenAIResponsesHandler(options)
	case ProtocolOpenAIChatCompletions:
		return newOpenAIChatHandler(options)
	case ProtocolAnthropicMessages:
		return newAnthropicHandler(options)
	case ProtocolGoogleGenerateContent:
		return newGoogleHandler(options)
	default:
		panic("llmusage: unsupported normalized protocol")
	}
}

type autoHandler struct {
	candidates []protocolHandler
	protocols  []Protocol
	selected   int
	recognized bool
}

func newAutoHandler(options Options) *autoHandler {
	protocols := []Protocol{ProtocolOpenAIResponses, ProtocolOpenAIChatCompletions, ProtocolAnthropicMessages, ProtocolGoogleGenerateContent}
	handler := &autoHandler{protocols: protocols, selected: -1}
	for _, protocol := range protocols {
		handler.candidates = append(handler.candidates, newExplicitHandler(protocol, options))
	}
	return handler
}

func (h *autoHandler) writeJSON(data []byte) error {
	for _, candidate := range h.candidates {
		if err := candidate.writeJSON(data); err != nil {
			return err
		}
	}
	return nil
}

func (h *autoHandler) finishJSON(sequence uint64) ([]Result, bool, error) {
	var matches []Result
	match := -1
	for index, candidate := range h.candidates {
		results, recognized, err := candidate.finishJSON(sequence)
		if err != nil {
			return nil, false, err
		}
		if recognized {
			if match >= 0 {
				return nil, false, ErrUnsupported
			}
			match = index
			matches = results
		}
	}
	return matches, match >= 0, nil
}

func (h *autoHandler) writeEventData(data []byte) error {
	if h.selected >= 0 {
		return h.candidates[h.selected].writeEventData(data)
	}
	for _, candidate := range h.candidates {
		if err := candidate.writeEventData(data); err != nil {
			return err
		}
	}
	return nil
}

func (h *autoHandler) finishEvent(event framing.Event) ([]Result, bool, error) {
	if h.selected >= 0 {
		results, _, err := h.candidates[h.selected].finishEvent(event)
		return results, true, err
	}
	match := -1
	var matches []Result
	for index, candidate := range h.candidates {
		results, recognized, err := candidate.finishEvent(event)
		if err != nil && recognized {
			return nil, false, err
		}
		if recognized {
			if match >= 0 {
				return nil, false, ErrUnsupported
			}
			match = index
			matches = results
		}
	}
	if match >= 0 {
		h.selected = match
		h.recognized = true
	}
	return matches, match >= 0, nil
}

func (h *autoHandler) finishStream() ([]Result, error) {
	if h.selected < 0 {
		return nil, ErrUnsupported
	}
	return h.candidates[h.selected].finishStream()
}
