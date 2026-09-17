// Package toml is a small TOML parser covering what lockfiles and project
// manifests use: tables, arrays of tables, dotted keys and headers, basic and
// literal strings (single and multi-line), integers, floats, booleans, arrays
// and inline tables. Dates and times are not supported and produce an error.
//
// Anything the parser does not understand is reported as an *Error carrying
// the line number. It never guesses.
//
// Parsed documents use plain Go types: map[string]any for tables, []any for
// arrays (including arrays of tables, whose elements are map[string]any),
// string, int64, float64 and bool.
package toml

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Error is a parse error with the 1-based line where it was detected.
type Error struct {
	Line int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

type table struct {
	vals     map[string]any
	explicit bool // defined by a [header]
	dotted   bool // created by a dotted key
	inline   bool // inline table, closed for extension
}

type tableArray struct {
	tables []*table
}

func newTable() *table { return &table{vals: map[string]any{}} }

type parser struct {
	src  string
	pos  int
	line int
	root *table
	cur  *table
}

// Parse parses a TOML document.
func Parse(data []byte) (map[string]any, error) {
	s := string(data)
	s = strings.TrimPrefix(s, "\ufeff")
	if !utf8.ValidString(s) {
		return nil, &Error{Line: 1, Msg: "document is not valid UTF-8"}
	}
	p := &parser{src: s, line: 1, root: newTable()}
	p.cur = p.root
	if err := p.parse(); err != nil {
		return nil, err
	}
	return convertTable(p.root), nil
}

func (p *parser) errf(format string, args ...any) error {
	return &Error{Line: p.line, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) eof() bool { return p.pos >= len(p.src) }

func (p *parser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) hasPrefix(s string) bool { return strings.HasPrefix(p.src[p.pos:], s) }

func (p *parser) skipSpace() {
	for !p.eof() && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
		p.pos++
	}
}

// skipComment consumes a comment up to (not including) the line break.
func (p *parser) skipComment() error {
	if p.peek() != '#' {
		return nil
	}
	for !p.eof() && p.src[p.pos] != '\n' {
		c := p.src[p.pos]
		if c == '\r' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '\n' {
			break
		}
		if (c < 0x20 && c != '\t') || c == 0x7f {
			return p.errf("control character in comment")
		}
		p.pos++
	}
	return nil
}

// newline consumes one line break if present and reports whether it did.
func (p *parser) newline() (bool, error) {
	if p.hasPrefix("\r\n") {
		p.pos += 2
		p.line++
		return true, nil
	}
	if p.peek() == '\n' {
		p.pos++
		p.line++
		return true, nil
	}
	if p.peek() == '\r' {
		return false, p.errf("bare carriage return")
	}
	return false, nil
}

// skipBlank skips whitespace, comments and newlines.
func (p *parser) skipBlank() error {
	for {
		p.skipSpace()
		if err := p.skipComment(); err != nil {
			return err
		}
		nl, err := p.newline()
		if err != nil {
			return err
		}
		if !nl {
			return nil
		}
	}
}

// endOfLine requires only whitespace and an optional comment before the next
// line break or end of input.
func (p *parser) endOfLine() error {
	p.skipSpace()
	if err := p.skipComment(); err != nil {
		return err
	}
	if p.eof() {
		return nil
	}
	nl, err := p.newline()
	if err != nil {
		return err
	}
	if !nl {
		return p.errf("unexpected %q after value", p.peek())
	}
	return nil
}

func (p *parser) parse() error {
	for {
		if err := p.skipBlank(); err != nil {
			return err
		}
		if p.eof() {
			return nil
		}
		if p.hasPrefix("[[") {
			if err := p.parseArrayTableHeader(); err != nil {
				return err
			}
		} else if p.peek() == '[' {
			if err := p.parseTableHeader(); err != nil {
				return err
			}
		} else {
			if err := p.parseKeyValue(p.cur); err != nil {
				return err
			}
		}
		if err := p.endOfLine(); err != nil {
			return err
		}
	}
}

func isBareKeyChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

func (p *parser) parseKey() ([]string, error) {
	var parts []string
	for {
		p.skipSpace()
		var part string
		switch c := p.peek(); {
		case c == '"':
			if p.hasPrefix(`"""`) {
				return nil, p.errf("multi-line strings cannot be keys")
			}
			s, err := p.parseBasicString()
			if err != nil {
				return nil, err
			}
			part = s
		case c == '\'':
			if p.hasPrefix("'''") {
				return nil, p.errf("multi-line strings cannot be keys")
			}
			s, err := p.parseLiteralString()
			if err != nil {
				return nil, err
			}
			part = s
		case isBareKeyChar(c):
			start := p.pos
			for !p.eof() && isBareKeyChar(p.src[p.pos]) {
				p.pos++
			}
			part = p.src[start:p.pos]
		case c == 0:
			return nil, p.errf("expected a key, found end of input")
		default:
			return nil, p.errf("expected a key, found %q", c)
		}
		parts = append(parts, part)
		p.skipSpace()
		if p.peek() != '.' {
			return parts, nil
		}
		p.pos++
	}
}

func (p *parser) parseTableHeader() error {
	p.pos++ // [
	keys, err := p.parseKey()
	if err != nil {
		return err
	}
	p.skipSpace()
	if p.peek() != ']' {
		return p.errf("expected ']' to close table header")
	}
	p.pos++
	parent, err := p.walkHeader(keys[:len(keys)-1])
	if err != nil {
		return err
	}
	last := keys[len(keys)-1]
	switch v := parent.vals[last].(type) {
	case nil:
		t := newTable()
		t.explicit = true
		parent.vals[last] = t
		p.cur = t
	case *table:
		if v.explicit || v.dotted || v.inline {
			return p.errf("table [%s] defined more than once", strings.Join(keys, "."))
		}
		v.explicit = true
		p.cur = v
	case *tableArray:
		return p.errf("[%s] is already an array of tables", strings.Join(keys, "."))
	default:
		return p.errf("key %q is already a value, cannot be a table", strings.Join(keys, "."))
	}
	return nil
}

func (p *parser) parseArrayTableHeader() error {
	p.pos += 2 // [[
	keys, err := p.parseKey()
	if err != nil {
		return err
	}
	p.skipSpace()
	if !p.hasPrefix("]]") {
		return p.errf("expected ']]' to close array of tables header")
	}
	p.pos += 2
	parent, err := p.walkHeader(keys[:len(keys)-1])
	if err != nil {
		return err
	}
	last := keys[len(keys)-1]
	t := newTable()
	t.explicit = true
	switch v := parent.vals[last].(type) {
	case nil:
		parent.vals[last] = &tableArray{tables: []*table{t}}
	case *tableArray:
		v.tables = append(v.tables, t)
	default:
		return p.errf("key %q is already defined and is not an array of tables", strings.Join(keys, "."))
	}
	p.cur = t
	return nil
}

// walkHeader resolves the intermediate keys of a header from the root,
// creating implicit tables and descending into the last element of arrays of
// tables.
func (p *parser) walkHeader(keys []string) (*table, error) {
	t := p.root
	for i, k := range keys {
		switch v := t.vals[k].(type) {
		case nil:
			n := newTable()
			t.vals[k] = n
			t = n
		case *table:
			if v.inline {
				return nil, p.errf("cannot extend inline table %q", strings.Join(keys[:i+1], "."))
			}
			t = v
		case *tableArray:
			t = v.tables[len(v.tables)-1]
		default:
			return nil, p.errf("key %q is already a value, cannot be a table", strings.Join(keys[:i+1], "."))
		}
	}
	return t, nil
}

func (p *parser) parseKeyValue(into *table) error {
	keys, err := p.parseKey()
	if err != nil {
		return err
	}
	p.skipSpace()
	if p.peek() != '=' {
		if p.eof() || p.peek() == '\n' || p.peek() == '\r' {
			return p.errf("expected '=' after key %q", strings.Join(keys, "."))
		}
		return p.errf("expected '=' after key %q, found %q", strings.Join(keys, "."), p.peek())
	}
	p.pos++
	p.skipSpace()
	val, err := p.parseValue()
	if err != nil {
		return err
	}
	t := into
	for i, k := range keys[:len(keys)-1] {
		switch v := t.vals[k].(type) {
		case nil:
			n := newTable()
			n.dotted = true
			t.vals[k] = n
			t = n
		case *table:
			if v.inline {
				return p.errf("cannot extend inline table %q", strings.Join(keys[:i+1], "."))
			}
			if v.explicit && !v.dotted {
				return p.errf("cannot add to table %q with a dotted key after its header", strings.Join(keys[:i+1], "."))
			}
			t = v
		default:
			return p.errf("key %q is already defined", strings.Join(keys[:i+1], "."))
		}
	}
	last := keys[len(keys)-1]
	if _, exists := t.vals[last]; exists {
		return p.errf("duplicate key %q", strings.Join(keys, "."))
	}
	t.vals[last] = val
	return nil
}

func (p *parser) parseValue() (any, error) {
	switch c := p.peek(); {
	case c == '"':
		if p.hasPrefix(`"""`) {
			return p.parseMultiBasicString()
		}
		return p.parseBasicString()
	case c == '\'':
		if p.hasPrefix("'''") {
			return p.parseMultiLiteralString()
		}
		return p.parseLiteralString()
	case c == '[':
		return p.parseArray()
	case c == '{':
		return p.parseInlineTable()
	case p.hasPrefix("true") && !p.bareContinues(4):
		p.pos += 4
		return true, nil
	case p.hasPrefix("false") && !p.bareContinues(5):
		p.pos += 5
		return false, nil
	case c == 0 || c == '\n' || c == '\r' || c == '#':
		return nil, p.errf("expected a value")
	default:
		return p.parseNumber()
	}
}

func (p *parser) bareContinues(n int) bool {
	return p.pos+n < len(p.src) && isBareKeyChar(p.src[p.pos+n])
}

func (p *parser) parseNumber() (any, error) {
	start := p.pos
	for !p.eof() {
		c := p.src[p.pos]
		if isBareKeyChar(c) || c == '+' || c == '.' || c == ':' {
			p.pos++
			continue
		}
		break
	}
	tok := p.src[start:p.pos]
	if tok == "" {
		return nil, p.errf("unexpected %q, expected a value", p.peek())
	}
	if strings.Contains(tok, ":") || looksLikeDate(tok) {
		return nil, p.errf("dates and times are not supported (%q)", tok)
	}
	switch tok {
	case "inf", "+inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	case "nan", "+nan", "-nan":
		return math.NaN(), nil
	}
	unsigned := strings.TrimLeft(tok, "+-")
	if unsigned == "" || unsigned[0] < '0' || unsigned[0] > '9' || len(tok)-len(unsigned) > 1 {
		return nil, p.errf("unexpected %q, expected a value", tok)
	}
	if len(unsigned) > 1 && unsigned[0] == '0' && (unsigned[1] == 'x' || unsigned[1] == 'o' || unsigned[1] == 'b') {
		if unsigned != tok {
			return nil, p.errf("invalid number %q: prefixed integers cannot have a sign", tok)
		}
		re, base := reHex, 16
		switch unsigned[1] {
		case 'o':
			re, base = reOct, 8
		case 'b':
			re, base = reBin, 2
		}
		if !re.MatchString(tok) {
			return nil, p.errf("invalid number %q", tok)
		}
		n, err := strconv.ParseInt(strings.ReplaceAll(tok[2:], "_", ""), base, 64)
		if err != nil {
			return nil, p.errf("invalid integer %q", tok)
		}
		return n, nil
	}
	clean := strings.ReplaceAll(tok, "_", "")
	switch {
	case reDecimal.MatchString(tok):
		n, err := strconv.ParseInt(clean, 10, 64)
		if err != nil {
			return nil, p.errf("invalid integer %q", tok)
		}
		return n, nil
	case reFloat.MatchString(tok) && strings.ContainsAny(tok, ".eE"):
		f, err := strconv.ParseFloat(clean, 64)
		if err != nil {
			return nil, p.errf("invalid float %q", tok)
		}
		return f, nil
	case len(unsigned) > 1 && unsigned[0] == '0' && unsigned[1] >= '0' && unsigned[1] <= '9':
		return nil, p.errf("invalid number %q: leading zeros", tok)
	case strings.ContainsAny(tok, ".eE"):
		return nil, p.errf("invalid float %q", tok)
	}
	return nil, p.errf("invalid number %q", tok)
}

var (
	reDecimal = regexp.MustCompile(`^[+-]?(0|[1-9](_?[0-9])*)$`)
	reFloat   = regexp.MustCompile(`^[+-]?(0|[1-9](_?[0-9])*)(\.[0-9](_?[0-9])*)?([eE][+-]?[0-9](_?[0-9])*)?$`)
	reHex     = regexp.MustCompile(`^0x[0-9A-Fa-f](_?[0-9A-Fa-f])*$`)
	reOct     = regexp.MustCompile(`^0o[0-7](_?[0-7])*$`)
	reBin     = regexp.MustCompile(`^0b[01](_?[01])*$`)
)

func looksLikeDate(tok string) bool {
	if len(tok) < 10 {
		return false
	}
	for i := 0; i < 10; i++ {
		c := tok[i]
		if i == 4 || i == 7 {
			if c != '-' {
				return false
			}
		} else if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (p *parser) parseBasicString() (string, error) {
	p.pos++ // "
	var b strings.Builder
	for {
		if p.eof() {
			return "", p.errf("unterminated string")
		}
		c := p.src[p.pos]
		switch {
		case c == '"':
			p.pos++
			return b.String(), nil
		case c == '\n' || c == '\r':
			return "", p.errf("unterminated string (newline in single-line string)")
		case c == '\\':
			if err := p.parseEscape(&b); err != nil {
				return "", err
			}
		case (c < 0x20 && c != '\t') || c == 0x7f:
			return "", p.errf("control character in string")
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
}

func (p *parser) parseEscape(b *strings.Builder) error {
	p.pos++ // backslash
	if p.eof() {
		return p.errf("unterminated escape sequence")
	}
	c := p.src[p.pos]
	p.pos++
	switch c {
	case 'b':
		b.WriteByte('\b')
	case 't':
		b.WriteByte('\t')
	case 'n':
		b.WriteByte('\n')
	case 'f':
		b.WriteByte('\f')
	case 'r':
		b.WriteByte('\r')
	case '"':
		b.WriteByte('"')
	case '\\':
		b.WriteByte('\\')
	case 'u', 'U':
		n := 4
		if c == 'U' {
			n = 8
		}
		if p.pos+n > len(p.src) {
			return p.errf("invalid unicode escape")
		}
		hex := p.src[p.pos : p.pos+n]
		v, err := strconv.ParseUint(hex, 16, 32)
		if err != nil || !utf8.ValidRune(rune(v)) {
			return p.errf("invalid unicode escape \\%c%s", c, hex)
		}
		b.WriteRune(rune(v))
		p.pos += n
	default:
		return p.errf("invalid escape sequence \\%c", c)
	}
	return nil
}

func (p *parser) parseLiteralString() (string, error) {
	p.pos++ // '
	start := p.pos
	for {
		if p.eof() {
			return "", p.errf("unterminated literal string")
		}
		c := p.src[p.pos]
		switch {
		case c == '\'':
			s := p.src[start:p.pos]
			p.pos++
			return s, nil
		case c == '\n' || c == '\r':
			return "", p.errf("unterminated literal string (newline in single-line string)")
		case (c < 0x20 && c != '\t') || c == 0x7f:
			return "", p.errf("control character in string")
		}
		p.pos++
	}
}

// skipLeadingNewline trims a newline immediately after an opening multi-line
// delimiter.
func (p *parser) skipLeadingNewline() {
	if p.hasPrefix("\r\n") {
		p.pos += 2
		p.line++
	} else if p.peek() == '\n' {
		p.pos++
		p.line++
	}
}

// closeMulti handles a run of quote characters inside a multi-line string.
// It reports whether the string ended, writing up to two literal quotes.
func (p *parser) closeMulti(q byte, b *strings.Builder) (bool, error) {
	n := 0
	for p.pos+n < len(p.src) && p.src[p.pos+n] == q {
		n++
	}
	if n < 3 {
		for i := 0; i < n; i++ {
			b.WriteByte(q)
		}
		p.pos += n
		return false, nil
	}
	if n > 5 {
		return false, p.errf("too many quotes at end of multi-line string")
	}
	for i := 0; i < n-3; i++ {
		b.WriteByte(q)
	}
	p.pos += n
	return true, nil
}

func (p *parser) parseMultiBasicString() (string, error) {
	p.pos += 3
	p.skipLeadingNewline()
	var b strings.Builder
	for {
		if p.eof() {
			return "", p.errf("unterminated multi-line string")
		}
		c := p.src[p.pos]
		switch {
		case c == '"':
			done, err := p.closeMulti('"', &b)
			if err != nil {
				return "", err
			}
			if done {
				return b.String(), nil
			}
		case c == '\\':
			// Line-ending backslash: trim whitespace and newlines.
			j := p.pos + 1
			for j < len(p.src) && (p.src[j] == ' ' || p.src[j] == '\t') {
				j++
			}
			if j < len(p.src) && (p.src[j] == '\n' || p.src[j] == '\r') {
				p.pos = j
				for !p.eof() {
					if nl, err := p.newline(); err != nil {
						return "", err
					} else if nl {
						continue
					}
					if p.peek() == ' ' || p.peek() == '\t' {
						p.pos++
						continue
					}
					break
				}
				continue
			}
			if err := p.parseEscape(&b); err != nil {
				return "", err
			}
		case c == '\n' || c == '\r':
			if _, err := p.newline(); err != nil {
				return "", err
			}
			b.WriteByte('\n')
		case (c < 0x20 && c != '\t') || c == 0x7f:
			return "", p.errf("control character in string")
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
}

func (p *parser) parseMultiLiteralString() (string, error) {
	p.pos += 3
	p.skipLeadingNewline()
	var b strings.Builder
	for {
		if p.eof() {
			return "", p.errf("unterminated multi-line literal string")
		}
		c := p.src[p.pos]
		switch {
		case c == '\'':
			done, err := p.closeMulti('\'', &b)
			if err != nil {
				return "", err
			}
			if done {
				return b.String(), nil
			}
		case c == '\n' || c == '\r':
			if _, err := p.newline(); err != nil {
				return "", err
			}
			b.WriteByte('\n')
		case (c < 0x20 && c != '\t') || c == 0x7f:
			return "", p.errf("control character in string")
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
}

func (p *parser) parseArray() ([]any, error) {
	startLine := p.line
	p.pos++ // [
	arr := []any{}
	for {
		if err := p.skipBlank(); err != nil {
			return nil, err
		}
		if p.eof() {
			return nil, &Error{Line: startLine, Msg: "unterminated array"}
		}
		if p.peek() == ']' {
			p.pos++
			return arr, nil
		}
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		if err := p.skipBlank(); err != nil {
			return nil, err
		}
		switch p.peek() {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return arr, nil
		case 0:
			return nil, &Error{Line: startLine, Msg: "unterminated array"}
		default:
			return nil, p.errf("expected ',' or ']' in array, found %q", p.peek())
		}
	}
}

// parseInlineTable parses { k = v, ... }. Newlines and a trailing comma are
// accepted (as TOML 1.1 allows) so hand-written manifests do not fail.
func (p *parser) parseInlineTable() (*table, error) {
	startLine := p.line
	p.pos++ // {
	t := newTable()
	for {
		if err := p.skipBlank(); err != nil {
			return nil, err
		}
		if p.eof() {
			return nil, &Error{Line: startLine, Msg: "unterminated inline table"}
		}
		if p.peek() == '}' {
			p.pos++
			t.inline = true
			return t, nil
		}
		if err := p.parseKeyValue(t); err != nil {
			return nil, err
		}
		if err := p.skipBlank(); err != nil {
			return nil, err
		}
		switch p.peek() {
		case ',':
			p.pos++
		case '}':
			p.pos++
			t.inline = true
			return t, nil
		case 0:
			return nil, &Error{Line: startLine, Msg: "unterminated inline table"}
		default:
			return nil, p.errf("expected ',' or '}' in inline table, found %q", p.peek())
		}
	}
}

func convertTable(t *table) map[string]any {
	m := make(map[string]any, len(t.vals))
	for k, v := range t.vals {
		m[k] = convertValue(v)
	}
	return m
}

func convertValue(v any) any {
	switch x := v.(type) {
	case *table:
		// Inline tables nested in dotted-key tables must also be closed; the
		// public representation does not distinguish them.
		return convertTable(x)
	case *tableArray:
		out := make([]any, len(x.tables))
		for i, t := range x.tables {
			out[i] = convertTable(t)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = convertValue(e)
		}
		return out
	default:
		return v
	}
}
