package llmusage

import (
	"errors"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/framing"
	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsoncapture"
)

// Decoder incrementally extracts usage from one response body. A Decoder is
// not safe for concurrent use.
type Decoder struct {
	options          Options
	handler          protocolHandler
	sse              *framing.Parser
	pending          []Result
	finished         bool
	terminal         error
	offset           int64
	eventPrefix      [len("[DONE]")]byte
	eventPrefixLen   int
	eventPassthrough bool
}

type protocolHandler interface {
	writeJSON([]byte) error
	finishJSON(uint64) ([]Result, bool, error)
	writeEventData([]byte) error
	finishEvent(framing.Event) ([]Result, bool, error)
	finishStream() ([]Result, error)
}

// NewDecoder creates a decoder for one response.
func NewDecoder(options Options) (*Decoder, error) {
	normalized, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	handler := newProtocolHandler(normalized)
	decoder := &Decoder{options: normalized, handler: handler}
	if normalized.Format == FormatSSE {
		decoder.sse = framing.NewParser(normalized.MaxFrameBytes, decoder.feedEventData, decoder.finishEvent)
	}
	return decoder, nil
}

// Feed consumes a response body chunk. The decoder does not retain data.
func (d *Decoder) Feed(data []byte) ([]Result, error) {
	if d == nil {
		return nil, &ParseError{Stage: "decoder", Err: ErrInvalidOptions}
	}
	if d.finished {
		return nil, &ParseError{Protocol: d.options.Protocol, Format: d.options.Format, Stage: "lifecycle", Offset: d.offset, Err: ErrFinished}
	}
	if d.terminal != nil {
		return nil, d.terminal
	}
	if len(data) == 0 {
		return nil, nil
	}
	d.offset += int64(len(data))
	var err error
	if d.options.Format == FormatJSON {
		err = d.handler.writeJSON(data)
	} else {
		err = d.sse.Feed(data)
	}
	if err != nil {
		return nil, d.fail("decode", d.offsetForError(), err)
	}
	return d.takePending(), nil
}

// Finish finalizes the response at EOF or body close. Repeated calls return no
// results and no error.
func (d *Decoder) Finish() ([]Result, error) {
	if d == nil {
		return nil, &ParseError{Stage: "decoder", Err: ErrInvalidOptions}
	}
	if d.finished {
		return nil, nil
	}
	d.finished = true
	if d.terminal != nil {
		return nil, d.terminal
	}
	if d.offset == 0 {
		return nil, nil
	}
	var err error
	if d.options.Format == FormatJSON {
		var results []Result
		var recognized bool
		results, recognized, err = d.handler.finishJSON(1)
		if err == nil && d.options.Protocol == ProtocolAuto && !recognized {
			err = ErrUnsupported
		}
		d.pending = append(d.pending, results...)
	} else {
		err = d.sse.Finish()
		if err == nil {
			var results []Result
			results, err = d.handler.finishStream()
			d.pending = append(d.pending, results...)
		}
	}
	if err != nil {
		return nil, d.fail("finish", d.offsetForError(), err)
	}
	return d.takePending(), nil
}

// Parse extracts all usage results from an in-memory response.
func Parse(data []byte, options Options) ([]Result, error) {
	decoder, err := NewDecoder(options)
	if err != nil {
		return nil, err
	}
	results, err := decoder.Feed(data)
	if err != nil {
		return nil, err
	}
	final, err := decoder.Finish()
	if err != nil {
		return nil, err
	}
	return append(results, final...), nil
}

func (d *Decoder) feedEventData(data []byte) error {
	if d.eventPassthrough {
		return d.handler.writeEventData(data)
	}
	needed := len(d.eventPrefix) - d.eventPrefixLen
	consumed := min(needed, len(data))
	copy(d.eventPrefix[d.eventPrefixLen:], data[:consumed])
	d.eventPrefixLen += consumed
	data = data[consumed:]
	if string(d.eventPrefix[:d.eventPrefixLen]) != "[DONE]"[:d.eventPrefixLen] || len(data) > 0 {
		d.eventPassthrough = true
		if err := d.handler.writeEventData(d.eventPrefix[:d.eventPrefixLen]); err != nil {
			return err
		}
		return d.handler.writeEventData(data)
	}
	return nil
}

func (d *Decoder) finishEvent(event framing.Event) error {
	if !d.eventPassthrough && d.eventPrefixLen == len("[DONE]") && string(d.eventPrefix[:]) == "[DONE]" {
		d.resetEventPrefix()
		return nil
	}
	if !d.eventPassthrough && d.eventPrefixLen > 0 {
		if err := d.handler.writeEventData(d.eventPrefix[:d.eventPrefixLen]); err != nil {
			return err
		}
	}
	d.resetEventPrefix()
	results, _, err := d.handler.finishEvent(event)
	if err == nil {
		d.pending = append(d.pending, results...)
	}
	return err
}

func (d *Decoder) resetEventPrefix() {
	d.eventPrefixLen = 0
	d.eventPassthrough = false
}

func (d *Decoder) takePending() []Result {
	if len(d.pending) == 0 {
		return nil
	}
	results := d.pending
	d.pending = nil
	return results
}

func (d *Decoder) offsetForError() int64 {
	if d.sse != nil {
		return d.sse.Offset()
	}
	return d.offset
}

func (d *Decoder) fail(stage string, offset int64, err error) error {
	mapped := ErrMalformedStream
	if errors.Is(err, jsoncapture.ErrLimit) || errors.Is(err, framing.ErrLimit) {
		mapped = ErrLimitExceeded
	} else if errors.Is(err, ErrUnsupported) {
		mapped = ErrUnsupported
	}
	parseErr := &ParseError{Protocol: d.options.Protocol, Format: d.options.Format, Stage: stage, Offset: offset, Err: errors.Join(mapped, err)}
	d.terminal = parseErr
	return parseErr
}
