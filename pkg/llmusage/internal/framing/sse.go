package framing

import (
	"errors"
	"strconv"
	"strings"
)

var (
	ErrMalformed = errors.New("malformed SSE stream")
	ErrLimit     = errors.New("SSE metadata exceeds limit")
)

type Event struct {
	Sequence    uint64
	Type        string
	ID          string
	RetryMillis *int64
	HasData     bool
}

type Parser struct {
	maxMetadataBytes int
	onData           func([]byte) error
	onEvent          func(Event) error

	field     []byte
	value     []byte
	state     lineState
	skipLF    bool
	dataLines int
	event     Event
	lastID    string
	finished  bool
	offset    int64
	sequence  uint64
	bom       []byte
	bomDone   bool
	dataBuf   []byte
}

type lineState uint8

const (
	lineField lineState = iota
	lineSkipSpace
	lineValue
	lineData
	lineIgnore
)

func NewParser(maxMetadataBytes int, onData func([]byte) error, onEvent func(Event) error) *Parser {
	return &Parser{maxMetadataBytes: maxMetadataBytes, onData: onData, onEvent: onEvent}
}

func (p *Parser) Offset() int64 { return p.offset }

func (p *Parser) Feed(data []byte) error {
	if p.finished {
		return ErrMalformed
	}
	for _, b := range data {
		p.offset++
		if !p.bomDone {
			ready, bytes := p.consumeBOM(b)
			if !ready {
				continue
			}
			for _, buffered := range bytes {
				if err := p.writeByte(buffered); err != nil {
					return err
				}
			}
			continue
		}
		if err := p.writeByte(b); err != nil {
			return err
		}
	}
	return nil
}

func (p *Parser) Finish() error {
	if p.finished {
		return nil
	}
	p.finished = true
	if !p.bomDone && len(p.bom) > 0 {
		for _, b := range p.bom {
			if err := p.writeByte(b); err != nil {
				return err
			}
		}
	}
	if len(p.field) > 0 || len(p.value) > 0 || p.state != lineField {
		if err := p.endLine(); err != nil {
			return err
		}
	}
	return p.dispatch()
}

func (p *Parser) consumeBOM(b byte) (bool, []byte) {
	p.bom = append(p.bom, b)
	want := []byte{0xef, 0xbb, 0xbf}
	for index := range p.bom {
		if p.bom[index] != want[index] {
			p.bomDone = true
			bytes := append([]byte(nil), p.bom...)
			p.bom = nil
			return true, bytes
		}
	}
	if len(p.bom) == len(want) {
		p.bomDone = true
		p.bom = nil
		return true, nil
	}
	return false, nil
}

func (p *Parser) writeByte(b byte) error {
	if p.skipLF {
		p.skipLF = false
		if b == '\n' {
			return nil
		}
	}
	if b == '\r' {
		if err := p.endLine(); err != nil {
			return err
		}
		p.skipLF = true
		return nil
	}
	if b == '\n' {
		return p.endLine()
	}

	switch p.state {
	case lineField:
		if b == ':' {
			return p.startValue()
		}
		return p.appendMetadata(&p.field, b)
	case lineSkipSpace:
		if err := p.beginValue(); err != nil {
			return err
		}
		if b == ' ' {
			return nil
		}
		return p.writeValueByte(b)
	case lineValue:
		return p.appendMetadata(&p.value, b)
	case lineData:
		return p.appendData(b)
	case lineIgnore:
	}
	return nil
}

func (p *Parser) writeValueByte(b byte) error {
	if p.state == lineData {
		return p.appendData(b)
	}
	if p.state == lineValue {
		return p.appendMetadata(&p.value, b)
	}
	return nil
}

func (p *Parser) startValue() error {
	key := string(p.field)
	p.field = p.field[:0]
	switch key {
	case "event", "id", "retry":
		p.field = append(p.field, key...)
		p.state = lineSkipSpace
	case "data":
		p.field = append(p.field, key...)
		p.state = lineSkipSpace
	default:
		p.state = lineIgnore
	}
	return nil
}

func (p *Parser) beginValue() error {
	if string(p.field) == "data" {
		if p.dataLines > 0 && p.onData != nil {
			if err := p.appendData('\n'); err != nil {
				return err
			}
		}
		p.dataLines++
		p.event.HasData = true
		p.state = lineData
		return nil
	}
	p.state = lineValue
	return nil
}

func (p *Parser) endLine() error {
	if p.state == lineField {
		if len(p.field) == 0 {
			if err := p.dispatch(); err != nil {
				return err
			}
		} else {
			key := string(p.field)
			switch key {
			case "data":
				if err := p.beginValue(); err != nil {
					return err
				}
			case "event":
				p.event.Type = ""
			case "id":
				p.lastID = ""
			}
		}
	} else if p.state == lineValue {
		switch string(p.field) {
		case "event":
			p.event.Type = string(p.value)
		case "id":
			if !strings.ContainsRune(string(p.value), '\x00') {
				p.lastID = string(p.value)
			}
		case "retry":
			value, err := strconv.ParseInt(string(p.value), 10, 64)
			if err == nil && value >= 0 {
				p.event.RetryMillis = &value
			}
		}
	} else if p.state == lineSkipSpace {
		key := string(p.field)
		if err := p.beginValue(); err != nil {
			return err
		}
		switch key {
		case "event":
			p.event.Type = ""
		case "id":
			p.lastID = ""
		}
	}
	p.field = p.field[:0]
	p.value = p.value[:0]
	p.state = lineField
	return nil
}

func (p *Parser) dispatch() error {
	if !p.event.HasData {
		p.resetEvent()
		return nil
	}
	if p.event.Type == "" {
		p.event.Type = "message"
	}
	p.sequence++
	p.event.Sequence = p.sequence
	p.event.ID = p.lastID
	if err := p.flushData(); err != nil {
		return err
	}
	if p.onEvent != nil {
		if err := p.onEvent(p.event); err != nil {
			return err
		}
	}
	p.resetEvent()
	return nil
}

func (p *Parser) resetEvent() {
	p.event = Event{}
	p.dataLines = 0
}

func (p *Parser) appendMetadata(dst *[]byte, b byte) error {
	if p.maxMetadataBytes > 0 && len(*dst)+1 > p.maxMetadataBytes {
		return ErrLimit
	}
	*dst = append(*dst, b)
	return nil
}

func (p *Parser) appendData(b byte) error {
	if p.onData == nil {
		return nil
	}
	p.dataBuf = append(p.dataBuf, b)
	if len(p.dataBuf) >= 8<<10 {
		return p.flushData()
	}
	return nil
}

func (p *Parser) flushData() error {
	if p.onData == nil || len(p.dataBuf) == 0 {
		p.dataBuf = p.dataBuf[:0]
		return nil
	}
	if err := p.onData(p.dataBuf); err != nil {
		return err
	}
	p.dataBuf = p.dataBuf[:0]
	return nil
}
