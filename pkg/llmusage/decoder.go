package llmusage

import (
	"errors"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/engine"
	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsonscan"
	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/protocol"
	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/sse"
)

// Decoder incrementally extracts usage from one response body. A Decoder is
// not safe for concurrent use.
type Decoder struct {
	options  Options
	engine   *engine.Decoder
	finished bool
	terminal error
}

// NewDecoder creates a decoder for one response.
func NewDecoder(options Options) (*Decoder, error) {
	normalized, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	core, err := engine.New(engine.Options{
		Protocol:      protocol.Kind(normalized.Protocol),
		Format:        engine.Format(normalized.Format),
		MaxFrameBytes: normalized.MaxFrameBytes,
		Limits:        protocol.Limits{MaxResultBytes: normalized.MaxResultBytes, MaxNestingDepth: normalized.MaxNestingDepth},
	})
	if err != nil {
		return nil, &ParseError{Protocol: normalized.Protocol, Format: normalized.Format, Stage: "options", Err: mapEngineError(err)}
	}
	return &Decoder{options: normalized, engine: core}, nil
}

// Feed consumes a response body chunk. The decoder does not retain data.
func (d *Decoder) Feed(data []byte) ([]Result, error) {
	if d == nil {
		return nil, &ParseError{Stage: "decoder", Err: ErrInvalidOptions}
	}
	if d.finished {
		return nil, &ParseError{Protocol: d.options.Protocol, Format: d.options.Format, Stage: "lifecycle", Offset: d.engine.Offset(), Err: ErrFinished}
	}
	if d.terminal != nil {
		return nil, d.terminal
	}
	results, err := d.engine.Feed(data)
	if err != nil {
		return nil, d.fail("decode", err)
	}
	return publicResults(results), nil
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
	results, err := d.engine.Finish()
	if err != nil {
		return nil, d.fail("finish", err)
	}
	return publicResults(results), nil
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

func publicResults(results []protocol.Result) []Result {
	if len(results) == 0 {
		return nil
	}
	converted := make([]Result, len(results))
	for index, result := range results {
		converted[index] = Result{
			Protocol: Protocol(result.Protocol), ResponseID: result.ResponseID, Model: result.Model,
			Usage:       Usage{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, TotalTokens: result.Usage.TotalTokens, CachedInputTokens: result.Usage.CachedInputTokens, CacheWriteTokens: result.Usage.CacheWriteTokens, ReasoningTokens: result.Usage.ReasoningTokens},
			TotalSource: TotalSource(result.TotalSource), RawUsage: result.RawUsage, Sequence: result.Sequence,
		}
	}
	return converted
}

func (d *Decoder) fail(stage string, err error) error {
	parseErr := &ParseError{Protocol: d.options.Protocol, Format: d.options.Format, Stage: stage, Offset: d.engine.Offset(), Err: mapEngineError(err)}
	d.terminal = parseErr
	return parseErr
}

func mapEngineError(err error) error {
	switch {
	case errors.Is(err, engine.ErrFinished):
		return ErrFinished
	case errors.Is(err, protocol.ErrUnsupported):
		return errors.Join(ErrUnsupported, err)
	case errors.Is(err, jsonscan.ErrLimit), errors.Is(err, sse.ErrLimit):
		return errors.Join(ErrLimitExceeded, err)
	default:
		return errors.Join(ErrMalformedStream, err)
	}
}
