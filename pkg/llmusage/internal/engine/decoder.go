package engine

import (
	"errors"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/protocol"
	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/sse"
)

var ErrFinished = errors.New("decoder finished")

type Format string

const (
	JSON Format = "json"
	SSE  Format = "sse"
)

type Options struct {
	Protocol            protocol.Kind
	Format              Format
	MaxSSEMetadataBytes int
	Limits              protocol.Limits
}

type Decoder struct {
	format   Format
	json     protocol.JSONDecoder
	stream   protocol.SSEDecoder
	sse      *sse.Parser
	pending  []protocol.Result
	finished bool
	terminal error
	offset   int64
}

func New(options Options) (*Decoder, error) {
	d := &Decoder{format: options.Format}
	var err error
	switch options.Format {
	case JSON:
		d.json, err = protocol.NewJSON(options.Protocol, options.Limits)
	case SSE:
		d.stream, err = protocol.NewSSE(options.Protocol, options.Limits)
		if err == nil {
			d.sse = sse.NewParser(options.MaxSSEMetadataBytes, d.feedEventData, d.finishEvent)
		}
	default:
		err = protocol.ErrUnsupported
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (d *Decoder) Feed(data []byte) ([]protocol.Result, error) {
	if d.finished {
		return nil, ErrFinished
	}
	if d.terminal != nil {
		return nil, d.terminal
	}
	if len(data) == 0 {
		return nil, nil
	}
	d.offset += int64(len(data))
	var err error
	if d.format == JSON {
		err = d.json.Feed(data)
	} else {
		err = d.sse.Feed(data)
	}
	if err != nil {
		d.terminal = err
		return nil, err
	}
	return d.takePending(), nil
}

func (d *Decoder) Finish() ([]protocol.Result, error) {
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
	var results []protocol.Result
	var err error
	if d.format == JSON {
		results, err = d.json.Finish()
	} else {
		err = d.sse.Finish()
		if err == nil {
			results, err = d.stream.FinishStream()
		}
	}
	if err != nil {
		d.terminal = err
		return nil, err
	}
	d.pending = append(d.pending, results...)
	return d.takePending(), nil
}

func (d *Decoder) Offset() int64 {
	if d.sse != nil {
		return d.sse.Offset()
	}
	return d.offset
}

func (d *Decoder) feedEventData(data []byte) error { return d.stream.FeedEventData(data) }
func (d *Decoder) finishEvent(event sse.Event) error {
	results, err := d.stream.FinishEvent(protocol.Event{Sequence: event.Sequence, Type: event.Type})
	if err == nil {
		d.pending = append(d.pending, results...)
	}
	return err
}
func (d *Decoder) takePending() []protocol.Result {
	if len(d.pending) == 0 {
		return nil
	}
	results := d.pending
	d.pending = nil
	return results
}
