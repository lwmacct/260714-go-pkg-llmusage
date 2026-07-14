package llmusage

const (
	defaultMaxFrameBytes   = 1 << 20
	defaultMaxResultBytes  = 64 << 10
	defaultMaxNestingDepth = 128
)

// Options configures one response decoder.
type Options struct {
	Protocol Protocol
	Format   Format
	// MaxFrameBytes limits retained SSE metadata and event identification data.
	// Zero uses the default of 1 MiB.
	MaxFrameBytes int
	// MaxResultBytes limits retained response identity and raw usage data.
	// Zero uses the default of 64 KiB.
	MaxResultBytes int
	// MaxNestingDepth limits all JSON nesting, including skipped values. Zero
	// uses the default of 128.
	MaxNestingDepth int
}

func normalizeOptions(options Options) (Options, error) {
	if options.Protocol == "" || options.Format == "" {
		return options, &ParseError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrInvalidOptions}
	}
	if options.Protocol != ProtocolOpenAIResponses {
		return options, &ParseError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrUnsupported}
	}
	if options.Format != FormatJSON && options.Format != FormatSSE {
		return options, &ParseError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrUnsupported}
	}
	if options.MaxFrameBytes < 0 || options.MaxResultBytes < 0 || options.MaxNestingDepth < 0 {
		return options, &ParseError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrInvalidOptions}
	}
	if options.MaxFrameBytes == 0 {
		options.MaxFrameBytes = defaultMaxFrameBytes
	}
	if options.MaxResultBytes == 0 {
		options.MaxResultBytes = defaultMaxResultBytes
	}
	if options.MaxNestingDepth == 0 {
		options.MaxNestingDepth = defaultMaxNestingDepth
	}
	return options, nil
}
