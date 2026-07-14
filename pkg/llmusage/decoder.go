package llmusage

import (
	"errors"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/framing"
	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsoncapture"
)

// Decoder incrementally extracts usage from one response body. A Decoder is
// not safe for concurrent use.
type Decoder struct {
	options  Options
	json     *jsoncapture.Scanner
	sse      *framing.Parser
	event    eventCapture
	pending  []Result
	finished bool
	terminal error
	offset   int64
}

type eventCapture struct {
	response    *jsoncapture.Scanner
	typeName    *jsoncapture.Scanner
	responseErr error
	typeErr     error
}

// NewDecoder creates a decoder for one response.
func NewDecoder(options Options) (*Decoder, error) {
	normalized, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	decoder := &Decoder{options: normalized}
	switch normalized.Format {
	case FormatJSON:
		decoder.json = newResponseScanner(nil, normalized.MaxResultBytes, normalized.MaxNestingDepth)
	case FormatSSE:
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
	switch d.options.Format {
	case FormatJSON:
		err = d.json.Write(data)
	case FormatSSE:
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
	var err error
	switch d.options.Format {
	case FormatJSON:
		if d.offset == 0 {
			return nil, nil
		}
		var captured jsoncapture.Result
		captured, err = d.json.Finish()
		if err == nil {
			var result Result
			var ok bool
			result, ok, err = resultFromFields(ProtocolOpenAIResponses, captured.Fields, 1)
			if err == nil && ok {
				d.pending = append(d.pending, result)
			}
		}
	case FormatSSE:
		err = d.sse.Finish()
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
	if d.event.response == nil {
		d.event.response = newResponseScanner([]string{"response"}, d.options.MaxResultBytes, d.options.MaxNestingDepth)
		d.event.typeName = jsoncapture.NewScanner(jsoncapture.Options{Fields: []string{"type"}, MaxBytes: d.options.MaxFrameBytes, MaxDepth: d.options.MaxNestingDepth})
	}
	if d.event.responseErr == nil {
		d.event.responseErr = d.event.response.Write(data)
	}
	if d.event.typeErr == nil {
		d.event.typeErr = d.event.typeName.Write(data)
	}
	return nil
}

func (d *Decoder) finishEvent(event framing.Event) error {
	capture := d.event
	d.event = eventCapture{}
	completed := event.Type == "response.completed"
	if capture.typeName != nil {
		if capture.typeErr == nil {
			_, capture.typeErr = capture.typeName.Finish()
		}
		completed = completed || rawString(capture.typeName.Captured("type")) == "response.completed"
	}
	if !completed {
		return nil
	}
	if capture.typeErr != nil {
		return capture.typeErr
	}
	if capture.responseErr != nil {
		return capture.responseErr
	}
	if capture.response == nil {
		return jsoncapture.ErrMalformed
	}
	fields, err := capture.response.Finish()
	if err != nil {
		return err
	}
	result, ok, err := resultFromFields(ProtocolOpenAIResponses, fields.Fields, event.Sequence)
	if err != nil {
		return err
	}
	if ok {
		d.pending = append(d.pending, result)
	}
	return nil
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
	if d.json != nil {
		return d.json.Offset()
	}
	return d.offset
}

func (d *Decoder) fail(stage string, offset int64, err error) error {
	mapped := ErrMalformedStream
	if errors.Is(err, jsoncapture.ErrLimit) || errors.Is(err, framing.ErrLimit) {
		mapped = ErrLimitExceeded
	}
	parseErr := &ParseError{Protocol: d.options.Protocol, Format: d.options.Format, Stage: stage, Offset: offset, Err: errors.Join(mapped, err)}
	d.terminal = parseErr
	return parseErr
}

func newResponseScanner(path []string, maxBytes, maxDepth int) *jsoncapture.Scanner {
	return jsoncapture.NewScanner(jsoncapture.Options{
		ObjectPath: path,
		Fields:     []string{"id", "model", "usage"},
		MaxBytes:   maxBytes,
		MaxDepth:   maxDepth,
	})
}
