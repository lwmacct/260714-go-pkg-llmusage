package llmusage

const (
	defaultMaxSSEMetadataBytes = 64 << 10
	defaultMaxResultBytes      = 64 << 10
	defaultMaxNestingDepth     = 128
)

// Options configures one response decoder.
type Options struct {
	Protocol Protocol
	Format   Format
	// MaxSSEMetadataBytes limits cumulative SSE metadata retained for one
	// event. It does not limit streamed data fields. Zero uses 64 KiB.
	MaxSSEMetadataBytes int
	// MaxResultBytes limits protocol detection fields, response identity, and
	// raw usage retained across a decoder. Zero uses the default of 64 KiB.
	MaxResultBytes int
	// MaxNestingDepth limits all JSON nesting, including skipped values. Zero
	// uses the default of 128.
	MaxNestingDepth int
}

func normalizeOptions(options Options) (Options, error) {
	if options.Protocol == "" || options.Format == "" {
		return options, &ParseError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrInvalidOptions}
	}
	switch options.Protocol {
	case ProtocolAuto, ProtocolOpenAIResponses, ProtocolOpenAIChatCompletions, ProtocolAnthropicMessages, ProtocolGoogleGenerateContent:
	default:
		return options, &ParseError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrUnsupported}
	}
	if options.Format != FormatJSON && options.Format != FormatSSE {
		return options, &ParseError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrUnsupported}
	}
	if options.MaxSSEMetadataBytes < 0 || options.MaxResultBytes < 0 || options.MaxNestingDepth < 0 {
		return options, &ParseError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrInvalidOptions}
	}
	if options.MaxSSEMetadataBytes == 0 {
		options.MaxSSEMetadataBytes = defaultMaxSSEMetadataBytes
	}
	if options.MaxResultBytes == 0 {
		options.MaxResultBytes = defaultMaxResultBytes
	}
	if options.MaxNestingDepth == 0 {
		options.MaxNestingDepth = defaultMaxNestingDepth
	}
	return options, nil
}
