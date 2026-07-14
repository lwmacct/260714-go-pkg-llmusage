package jsonscan

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

var (
	ErrMalformed = errors.New("malformed JSON")
	ErrLimit     = errors.New("captured JSON exceeds limit")
)

type Options struct {
	ObjectPath []string
	Fields     []string
	MaxBytes   int
	MaxDepth   int
	Budget     *Budget
}

// Budget bounds captured bytes shared by multiple scanners. It is not safe
// for concurrent use.
type Budget struct {
	max  int
	used int
}

func NewBudget(max int) *Budget { return &Budget{max: max} }

func (b *Budget) reserve(bytes int) bool {
	if b == nil || b.max <= 0 {
		return true
	}
	if bytes < 0 || b.used > b.max-bytes {
		return false
	}
	b.used += bytes
	return true
}

func (b *Budget) release(bytes int) {
	if b == nil {
		return
	}
	b.used -= bytes
	if b.used < 0 {
		b.used = 0
	}
}

type Result struct {
	Fields map[string]json.RawMessage
	Found  bool
}

const (
	jsonObject byte = '{'
	jsonArray  byte = '['

	objExpectKey = iota
	objExpectColon
	objExpectValue
	objAfterValue
	arrayExpectValue
	arrayAfterValue
)

type jsonContext struct {
	kind       byte
	state      int
	pendingKey string
	pathMatch  int
	canClose   bool
}

// Scanner selectively retains direct fields from one object while validating
// the retained JSON values. Skipped containers are traversed without buffering.
type Scanner struct {
	stack []jsonContext

	inString       bool
	escape         bool
	unicodeDigits  int
	captureString  bool
	stringBuf      []byte
	stringReserved int
	inPrimitive    bool
	primitive      primitiveState

	objectPath []string
	fields     map[string]struct{}
	result     map[string]json.RawMessage
	found      bool
	maxBytes   int
	maxDepth   int
	usedBytes  int
	budget     *Budget
	reserved   int
	err        error
	rootSeen   bool
	rootDone   bool
	offset     int64

	capturingValue bool
	valueKey       string
	valueDepth     int
	valueInString  bool
	valueEscape    bool
	valuePrimitive bool
	valueBuf       []byte
}

type primitiveState struct {
	kind    byte
	stage   uint8
	literal string
	index   int
}

func NewScanner(options Options) *Scanner {
	path := append([]string(nil), options.ObjectPath...)
	fields := make(map[string]struct{}, len(options.Fields))
	for _, field := range options.Fields {
		fields[field] = struct{}{}
	}
	return &Scanner{objectPath: path, fields: fields, maxBytes: options.MaxBytes, maxDepth: options.MaxDepth, budget: options.Budget}
}

// Release removes this scanner's captured bytes from its shared budget. The
// scanner must not be used afterward.
func (s *Scanner) Release() {
	if s == nil {
		return
	}
	s.budget.release(s.reserved + s.stringReserved)
	s.reserved = 0
	s.stringReserved = 0
}

func (s *Scanner) Write(data []byte) error {
	if s.err != nil {
		return s.err
	}
	for _, b := range data {
		s.offset++
		if err := s.writeByte(b); err != nil {
			s.err = err
			return err
		}
	}
	return nil
}

func (s *Scanner) Offset() int64 { return s.offset }

func (s *Scanner) Captured(name string) json.RawMessage {
	return append(json.RawMessage(nil), s.result[name]...)
}

func (s *Scanner) Finish() (Result, error) {
	if s.err != nil {
		return Result{}, s.err
	}
	if s.capturingValue {
		if !s.valuePrimitive {
			return Result{}, malformed("incomplete captured value")
		}
		if err := s.finishValueCapture(); err != nil {
			return Result{}, err
		}
	}
	if s.inPrimitive {
		if !s.primitive.complete() {
			return Result{}, malformed("invalid primitive")
		}
		s.inPrimitive = false
		s.primitive = primitiveState{}
		s.completeValue()
	}
	if s.inString || len(s.stack) != 0 || !s.rootSeen || !s.rootDone {
		return Result{}, malformed("incomplete JSON document")
	}
	return Result{Fields: s.result, Found: s.found}, nil
}

func (s *Scanner) writeByte(b byte) error {
	if s.rootDone {
		if isJSONWhitespace(b) {
			return nil
		}
		return malformed("multiple JSON values")
	}
	if s.capturingValue {
		reprocess, err := s.writeValueByte(b)
		if err != nil || !reprocess {
			return err
		}
	}
	if s.inString {
		return s.writeStringByte(b)
	}
	if s.inPrimitive {
		if !isJSONDelimiter(b) {
			return s.primitive.write(b)
		}
		if !s.primitive.complete() {
			return malformed("invalid primitive")
		}
		s.inPrimitive = false
		s.primitive = primitiveState{}
		s.completeValue()
		if len(s.stack) == 0 {
			s.rootDone = true
		}
		if isJSONWhitespace(b) {
			return nil
		}
	}
	if isJSONWhitespace(b) {
		return nil
	}

	if len(s.stack) == 0 && s.rootSeen {
		return malformed("multiple JSON values")
	}
	switch b {
	case '"':
		if err := s.requireStringPosition(); err != nil {
			return err
		}
		s.markRootSeen()
		if s.shouldCaptureValue() {
			return s.startValueCapture(b)
		}
		s.startString()
	case '{':
		if err := s.requireValuePosition(); err != nil {
			return err
		}
		s.markRootSeen()
		if s.shouldCaptureValue() {
			return s.startValueCapture(b)
		}
		if err := s.startObject(); err != nil {
			return err
		}
	case '[':
		if err := s.requireValuePosition(); err != nil {
			return err
		}
		s.markRootSeen()
		if s.shouldCaptureValue() {
			return s.startValueCapture(b)
		}
		if err := s.startArray(); err != nil {
			return err
		}
	case ':':
		ctx := s.top()
		if ctx == nil || ctx.kind != jsonObject || ctx.state != objExpectColon {
			return malformed("unexpected colon")
		}
		ctx.state = objExpectValue
	case ',':
		if err := s.nextValue(); err != nil {
			return err
		}
	case '}':
		if err := s.endContainer(jsonObject); err != nil {
			return err
		}
	case ']':
		if err := s.endContainer(jsonArray); err != nil {
			return err
		}
	default:
		if err := s.requireValuePosition(); err != nil {
			return err
		}
		s.markRootSeen()
		if s.shouldCaptureValue() {
			return s.startValueCapture(b)
		}
		if !isPrimitiveStart(b) {
			return malformed("unexpected token")
		}
		s.inPrimitive = true
		if err := s.primitive.start(b); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scanner) markRootSeen() {
	if len(s.stack) == 0 {
		s.rootSeen = true
	}
}

func (s *Scanner) startString() {
	s.inString = true
	s.escape = false
	s.unicodeDigits = 0
	s.captureString = s.shouldCaptureString()
	if s.captureString {
		s.stringBuf = append(s.stringBuf[:0], '"')
	}
}

func (s *Scanner) writeStringByte(b byte) error {
	if s.captureString {
		if err := s.appendString(b); err != nil {
			return err
		}
	}
	if s.unicodeDigits > 0 {
		if !isHexDigit(b) {
			return malformed("invalid unicode escape")
		}
		s.unicodeDigits--
		return nil
	}
	if s.escape {
		s.escape = false
		if b == 'u' {
			s.unicodeDigits = 4
			return nil
		}
		if !isSimpleEscape(b) {
			return malformed("invalid string escape")
		}
		return nil
	}
	if b == '\\' {
		s.escape = true
		return nil
	}
	if b != '"' {
		if b < 0x20 {
			return malformed("unescaped control character")
		}
		return nil
	}

	raw := s.stringBuf
	captureString := s.captureString
	s.inString = false
	s.captureString = false
	s.budget.release(s.stringReserved)
	s.stringReserved = 0
	ctx := s.top()
	if ctx == nil {
		s.rootDone = true
		return nil
	}
	if ctx.kind == jsonObject && ctx.state == objExpectKey {
		if !captureString {
			ctx.pendingKey = ""
			ctx.state = objExpectColon
			return nil
		}
		decoded, err := strconv.Unquote(string(raw))
		if err != nil {
			return malformed("invalid object key")
		}
		ctx.pendingKey = decoded
		ctx.state = objExpectColon
		return nil
	}
	s.completeValue()
	return nil
}

func (s *Scanner) shouldCaptureString() bool {
	ctx := s.top()
	if ctx == nil || ctx.kind != jsonObject || ctx.state != objExpectKey {
		return false
	}
	if s.inTargetObject(ctx) {
		return true
	}
	return ctx.pathMatch < len(s.objectPath) && (ctx.pathMatch > 0 || len(s.stack) == 1)
}

func (s *Scanner) shouldCaptureValue() bool {
	ctx := s.top()
	if ctx == nil || ctx.kind != jsonObject || ctx.state != objExpectValue || !s.inTargetObject(ctx) {
		return false
	}
	_, ok := s.fields[ctx.pendingKey]
	return ok
}

func (s *Scanner) startObject() error {
	if s.maxDepth > 0 && len(s.stack)+1 > s.maxDepth {
		return ErrLimit
	}
	match := 0
	parent := s.top()
	if parent != nil && parent.kind == jsonObject && parent.state == objExpectValue &&
		parent.pathMatch < len(s.objectPath) && parent.pendingKey == s.objectPath[parent.pathMatch] {
		if parent.pathMatch > 0 || len(s.stack) == 1 {
			match = parent.pathMatch + 1
		}
	}
	s.stack = append(s.stack, jsonContext{kind: jsonObject, state: objExpectKey, pathMatch: match, canClose: true})
	if s.inTargetObject(s.top()) {
		s.found = true
	}
	return nil
}

func (s *Scanner) startArray() error {
	if s.maxDepth > 0 && len(s.stack)+1 > s.maxDepth {
		return ErrLimit
	}
	s.stack = append(s.stack, jsonContext{kind: jsonArray, state: arrayExpectValue, canClose: true})
	return nil
}

func (s *Scanner) endContainer(kind byte) error {
	ctx := s.top()
	if ctx == nil || ctx.kind != kind {
		return malformed("unexpected container close")
	}
	if (kind == jsonObject && ctx.state != objExpectKey && ctx.state != objAfterValue) ||
		(kind == jsonArray && ctx.state != arrayExpectValue && ctx.state != arrayAfterValue) {
		return malformed("incomplete container")
	}
	if !ctx.canClose {
		return malformed("trailing comma")
	}
	s.stack = s.stack[:len(s.stack)-1]
	if len(s.stack) == 0 {
		s.rootDone = true
		return nil
	}
	s.completeValue()
	return nil
}

func (s *Scanner) nextValue() error {
	ctx := s.top()
	if ctx == nil {
		return malformed("unexpected comma")
	}
	switch {
	case ctx.kind == jsonObject && ctx.state == objAfterValue:
		ctx.pendingKey = ""
		ctx.state = objExpectKey
		ctx.canClose = false
	case ctx.kind == jsonArray && ctx.state == arrayAfterValue:
		ctx.state = arrayExpectValue
		ctx.canClose = false
	default:
		return malformed("unexpected comma")
	}
	return nil
}

func (s *Scanner) completeValue() {
	ctx := s.top()
	if ctx == nil {
		s.rootDone = true
		return
	}
	switch ctx.kind {
	case jsonObject:
		if ctx.state == objExpectValue {
			ctx.pendingKey = ""
			ctx.state = objAfterValue
			ctx.canClose = true
		}
	case jsonArray:
		if ctx.state == arrayExpectValue {
			ctx.state = arrayAfterValue
			ctx.canClose = true
		}
	}
}

func (s *Scanner) startValueCapture(first byte) error {
	ctx := s.top()
	if (first == '{' || first == '[') && s.maxDepth > 0 && len(s.stack)+1 > s.maxDepth {
		return ErrLimit
	}
	s.capturingValue = true
	s.valueKey = ctx.pendingKey
	s.valueDepth = 0
	s.valueInString = false
	s.valueEscape = false
	s.valuePrimitive = false
	s.valueBuf = s.valueBuf[:0]
	if err := s.appendValue(first); err != nil {
		return err
	}
	switch first {
	case '"':
		s.valueInString = true
	case '{', '[':
		s.valueDepth = 1
	default:
		if !isPrimitiveStart(first) {
			return malformed("invalid captured value")
		}
		s.valuePrimitive = true
	}
	return nil
}

func (s *Scanner) writeValueByte(b byte) (bool, error) {
	if s.valuePrimitive {
		if isJSONDelimiter(b) {
			if err := s.finishValueCapture(); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, s.appendValue(b)
	}
	if err := s.appendValue(b); err != nil {
		return false, err
	}
	if s.valueInString {
		if s.valueEscape {
			s.valueEscape = false
			return false, nil
		}
		if b == '\\' {
			s.valueEscape = true
			return false, nil
		}
		if b == '"' {
			s.valueInString = false
			if s.valueDepth == 0 {
				return false, s.finishValueCapture()
			}
		}
		return false, nil
	}
	switch b {
	case '"':
		s.valueInString = true
	case '{', '[':
		s.valueDepth++
		if s.maxDepth > 0 && len(s.stack)+s.valueDepth > s.maxDepth {
			return false, ErrLimit
		}
	case '}', ']':
		s.valueDepth--
		if s.valueDepth < 0 {
			return false, malformed("invalid captured container")
		}
		if s.valueDepth == 0 {
			return false, s.finishValueCapture()
		}
	}
	return false, nil
}

func (s *Scanner) finishValueCapture() error {
	raw := append(json.RawMessage(nil), s.valueBuf...)
	if !json.Valid(raw) {
		return malformed("invalid captured value")
	}
	if s.result == nil {
		s.result = make(map[string]json.RawMessage)
	}
	s.result[s.valueKey] = raw
	s.usedBytes += len(raw)
	s.capturingValue = false
	s.valueKey = ""
	s.valueBuf = s.valueBuf[:0]
	s.completeValue()
	return nil
}

func (s *Scanner) appendString(b byte) error {
	if s.maxBytes > 0 && s.usedBytes+len(s.stringBuf)+1 > s.maxBytes {
		return ErrLimit
	}
	if !s.budget.reserve(1) {
		return ErrLimit
	}
	s.stringReserved++
	s.stringBuf = append(s.stringBuf, b)
	return nil
}

func (s *Scanner) appendValue(b byte) error {
	if s.maxBytes > 0 && s.usedBytes+len(s.valueBuf)+1 > s.maxBytes {
		return ErrLimit
	}
	if !s.budget.reserve(1) {
		return ErrLimit
	}
	s.reserved++
	s.valueBuf = append(s.valueBuf, b)
	return nil
}

func (s *Scanner) requireValuePosition() error {
	ctx := s.top()
	if ctx == nil {
		return nil
	}
	if (ctx.kind == jsonObject && ctx.state == objExpectValue) ||
		(ctx.kind == jsonArray && ctx.state == arrayExpectValue) {
		return nil
	}
	return malformed("value without separator")
}

func (s *Scanner) requireStringPosition() error {
	ctx := s.top()
	if ctx != nil && ctx.kind == jsonObject && ctx.state == objExpectKey {
		return nil
	}
	return s.requireValuePosition()
}

func (s *Scanner) inTargetObject(ctx *jsonContext) bool {
	if ctx == nil || ctx.kind != jsonObject {
		return false
	}
	if len(s.objectPath) == 0 {
		return len(s.stack) == 1
	}
	return ctx.pathMatch == len(s.objectPath)
}

func (s *Scanner) top() *jsonContext {
	if len(s.stack) == 0 {
		return nil
	}
	return &s.stack[len(s.stack)-1]
}

func malformed(message string) error { return fmt.Errorf("%w: %s", ErrMalformed, message) }

func isPrimitiveStart(b byte) bool {
	return b == '-' || b == 't' || b == 'f' || b == 'n' || (b >= '0' && b <= '9')
}

func isSimpleEscape(b byte) bool {
	switch b {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		return true
	default:
		return false
	}
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func (p *primitiveState) start(b byte) error {
	*p = primitiveState{kind: b}
	switch b {
	case 't':
		p.literal = "true"
		p.index = 1
	case 'f':
		p.literal = "false"
		p.index = 1
	case 'n':
		p.literal = "null"
		p.index = 1
	case '-':
		p.stage = 0
	case '0':
		p.stage = 2
	default:
		if b >= '1' && b <= '9' {
			p.stage = 1
			return nil
		}
		return malformed("invalid primitive")
	}
	return nil
}

func (p *primitiveState) write(b byte) error {
	if p.literal != "" {
		if p.index >= len(p.literal) || b != p.literal[p.index] {
			return malformed("invalid literal")
		}
		p.index++
		return nil
	}
	switch p.stage {
	case 0: // Leading minus requires an integer digit.
		if b == '0' {
			p.stage = 2
			return nil
		}
		if b >= '1' && b <= '9' {
			p.stage = 1
			return nil
		}
	case 1: // Integer digits.
		if b >= '0' && b <= '9' {
			return nil
		}
		if b == '.' {
			p.stage = 3
			return nil
		}
		if b == 'e' || b == 'E' {
			p.stage = 5
			return nil
		}
	case 2: // Zero cannot have another integer digit.
		if b == '.' {
			p.stage = 3
			return nil
		}
		if b == 'e' || b == 'E' {
			p.stage = 5
			return nil
		}
	case 3: // Fraction requires at least one digit.
		if b >= '0' && b <= '9' {
			p.stage = 4
			return nil
		}
	case 4: // Fraction digits.
		if b >= '0' && b <= '9' {
			return nil
		}
		if b == 'e' || b == 'E' {
			p.stage = 5
			return nil
		}
	case 5: // Exponent sign or first digit.
		if b == '+' || b == '-' {
			p.stage = 6
			return nil
		}
		if b >= '0' && b <= '9' {
			p.stage = 7
			return nil
		}
	case 6: // Exponent sign requires a digit.
		if b >= '0' && b <= '9' {
			p.stage = 7
			return nil
		}
	case 7: // Exponent digits.
		if b >= '0' && b <= '9' {
			return nil
		}
	}
	return malformed("invalid number")
}

func (p primitiveState) complete() bool {
	if p.literal != "" {
		return p.index == len(p.literal)
	}
	return p.stage == 1 || p.stage == 2 || p.stage == 4 || p.stage == 7
}

func isJSONDelimiter(b byte) bool {
	return isJSONWhitespace(b) || b == ',' || b == '}' || b == ']'
}

func isJSONWhitespace(b byte) bool {
	switch b {
	case ' ', '\n', '\r', '\t':
		return true
	default:
		return false
	}
}
