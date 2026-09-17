package toml

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]any
	}{
		{"empty", "", map[string]any{}},
		{"comments only", "# hi\n\n  # there\n", map[string]any{}},
		{"basic string", `a = "hello"`, map[string]any{"a": "hello"}},
		{"escapes", `a = "q\" b\\ t\t n\n u\u00e9 U\U0001F600"`, map[string]any{"a": "q\" b\\ t\t n\n u\u00e9 U\U0001F600"}},
		{"literal string keeps backslashes", `a = 'C:\path\to'`, map[string]any{"a": `C:\path\to`}},
		{"hash inside string", `a = "x # not a comment" # comment`, map[string]any{"a": "x # not a comment"}},
		{"multi-line basic", "a = \"\"\"\nline1\nline2\"\"\"", map[string]any{"a": "line1\nline2"}},
		{"multi-line basic line continuation", "a = \"\"\"one \\\n   two\"\"\"", map[string]any{"a": "one two"}},
		{"multi-line basic trailing quotes", `a = """he said "hi"""""`, map[string]any{"a": `he said "hi""`}},
		{"multi-line literal", "a = '''\nraw \\n\n'''", map[string]any{"a": "raw \\n\n"}},
		{"crlf", "a = 1\r\nb = 2\r\n", map[string]any{"a": int64(1), "b": int64(2)}},
		{"integers", "a = 42\nb = -7\nc = 1_000\nd = 0xff\ne = 0o17\nf = 0b101\ng = +3", map[string]any{
			"a": int64(42), "b": int64(-7), "c": int64(1000), "d": int64(255), "e": int64(15), "f": int64(5), "g": int64(3)}},
		{"floats", "a = 1.5\nb = -2e3\nc = 6.02E+23", map[string]any{"a": 1.5, "b": -2000.0, "c": 6.02e23}},
		{"booleans", "a = true\nb = false", map[string]any{"a": true, "b": false}},
		{"quoted keys", `"a b" = 1` + "\n" + `'c.d' = 2`, map[string]any{"a b": int64(1), "c.d": int64(2)}},
		{"dotted keys", "a.b.c = 1\na.b.d = 2", map[string]any{"a": map[string]any{"b": map[string]any{"c": int64(1), "d": int64(2)}}}},
		{"dotted key with quotes and spaces", `a . "b.c" = 1`, map[string]any{"a": map[string]any{"b.c": int64(1)}}},
		{"table", "[server]\nhost = \"x\"\nport = 80", map[string]any{"server": map[string]any{"host": "x", "port": int64(80)}}},
		{"dotted table header", "[a.b.c]\nx = 1\n[a]\ny = 2", map[string]any{"a": map[string]any{"y": int64(2), "b": map[string]any{"c": map[string]any{"x": int64(1)}}}}},
		{"quoted table header", "[target.'cfg(unix)'.dependencies]\nlibc = \"0.2\"", map[string]any{
			"target": map[string]any{"cfg(unix)": map[string]any{"dependencies": map[string]any{"libc": "0.2"}}}}},
		{"array of tables", "[[package]]\nname = \"a\"\n\n[[package]]\nname = \"b\"", map[string]any{
			"package": []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}}}},
		{"sub-table of array element", "[[package]]\nname = \"a\"\n[package.dependencies]\nx = \"1\"\n[[package]]\nname = \"b\"\n[package.dependencies]\ny = \"2\"", map[string]any{
			"package": []any{
				map[string]any{"name": "a", "dependencies": map[string]any{"x": "1"}},
				map[string]any{"name": "b", "dependencies": map[string]any{"y": "2"}},
			}}},
		{"nested array of tables", "[[a]]\n[[a.b]]\nx = 1\n[[a.b]]\nx = 2", map[string]any{
			"a": []any{map[string]any{"b": []any{map[string]any{"x": int64(1)}, map[string]any{"x": int64(2)}}}}}},
		{"arrays", `a = [1, 2, 3]` + "\n" + `b = ["x", 'y']` + "\n" + `c = []` + "\n" + `d = [[1], ["a"]]`, map[string]any{
			"a": []any{int64(1), int64(2), int64(3)}, "b": []any{"x", "y"}, "c": []any{}, "d": []any{[]any{int64(1)}, []any{"a"}}}},
		{"multi-line array with comments and trailing comma", "a = [\n  \"x\", # first\n  # between\n  \"y\",\n]", map[string]any{"a": []any{"x", "y"}}},
		{"inline table", `a = { name = "serde", version = "1.0", nested = { x = 1 } }`, map[string]any{
			"a": map[string]any{"name": "serde", "version": "1.0", "nested": map[string]any{"x": int64(1)}}}},
		{"empty inline table", `a = {}`, map[string]any{"a": map[string]any{}}},
		{"inline table with dotted key", `a = { b.c = 1 }`, map[string]any{"a": map[string]any{"b": map[string]any{"c": int64(1)}}}},
		{"array of inline tables", "dependencies = [\n    { name = \"idna\" },\n    { name = \"numpy\", version = \"1.26.4\", source = { registry = \"https://pypi.org/simple\" }, marker = \"python_full_version < '3.12'\" },\n]", map[string]any{
			"dependencies": []any{
				map[string]any{"name": "idna"},
				map[string]any{"name": "numpy", "version": "1.26.4", "source": map[string]any{"registry": "https://pypi.org/simple"}, "marker": "python_full_version < '3.12'"},
			}}},
		{"multi-line inline table accepted", "a = {\n  x = 1,\n  y = 2,\n}", map[string]any{"a": map[string]any{"x": int64(1), "y": int64(2)}}},
		{"bom", "\ufeffa = 1", map[string]any{"a": int64(1)}},
		{"bare key that looks like number", "1234 = \"x\"", map[string]any{"1234": "x"}},
		{"key named true", "true = 1", map[string]any{"true": int64(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse([]byte(tt.in))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %#v\nwant %#v", got, tt.want)
			}
		})
	}
}

func TestParseSpecialFloats(t *testing.T) {
	got, err := Parse([]byte("a = inf\nb = -inf\nc = nan"))
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsInf(got["a"].(float64), 1) || !math.IsInf(got["b"].(float64), -1) || !math.IsNaN(got["c"].(float64)) {
		t.Errorf("got %#v", got)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		line int
		msg  string
	}{
		{"datetime", "a = 1\nb = 1979-05-27T07:32:00Z", 2, "dates and times are not supported"},
		{"date only", "b = 1979-05-27", 1, "dates and times are not supported"},
		{"time only", "\n\nb = 07:32:00", 3, "dates and times are not supported"},
		{"duplicate key", "a = 1\na = 2", 2, "duplicate key"},
		{"duplicate table", "[a]\nx = 1\n[a]\ny = 2", 3, "defined more than once"},
		{"table after array of tables", "[[a]]\n[a]", 2, "already an array of tables"},
		{"array of tables after table", "[a]\n[[a]]", 2, "not an array of tables"},
		{"value then table", "a = 1\n[a]", 2, "already a value"},
		{"unterminated string", "a = \"abc\nb = 1", 1, "unterminated string"},
		{"unterminated literal", "a = 'abc", 1, "unterminated literal string"},
		{"unterminated multi-line", "a = \"\"\"abc\n\n", 3, "unterminated multi-line string"},
		{"bad escape", `a = "\q"`, 1, "invalid escape"},
		{"bad unicode escape", `a = "\uZZZZ"`, 1, "invalid unicode escape"},
		{"missing equals", "\na \"x\"", 2, "expected '='"},
		{"missing value", "a = \n", 1, "expected a value"},
		{"garbage after value", "a = 1 2", 1, "after value"},
		{"unterminated array", "a = [1,\n2\n", 1, "unterminated array"},
		{"missing comma in array", "a = [1 2]", 1, "expected ',' or ']'"},
		{"unterminated inline table", "a = { x = 1", 1, "unterminated inline table"},
		{"extend inline table by header", "a = { x = 1 }\n[a.b]", 2, "cannot extend inline table"},
		{"extend inline table by dotted key", "a = { x = 1 }\na.y = 2", 2, "cannot extend inline table"},
		{"duplicate key in inline table", "a = { x = 1, x = 2 }", 1, "duplicate key"},
		{"unclosed header", "[a\nx = 1", 1, "expected ']'"},
		{"unclosed array header", "[[a]\n", 1, "expected ']]'"},
		{"leading zero", "a = 007", 1, "leading zeros"},
		{"bare word value", "a = yes", 1, "expected a value"},
		{"bare carriage return", "a = 1\rb = 2", 1, "bare carriage return"},
		{"control char in string", "a = \"x\x01\"", 1, "control character"},
		{"multi-line key", `"""a""" = 1`, 1, "multi-line strings cannot be keys"},
		{"dotted key into header table", "[a]\nx = 1\n[b]\n[c]\n", 0, ""},
		{"float with trailing dot", "a = 1.", 1, "invalid float"},
		{"double underscore", "a = 1__0", 1, "invalid number"},
		{"underscore before dot", "a = 1_.2", 1, "invalid float"},
		{"underscore after exponent", "a = 1e_2", 1, "invalid float"},
		{"trailing underscore", "a = 12_", 1, "invalid number"},
		{"signed hex digits", "a = 0x-1", 1, "invalid number"},
		{"underscore after prefix", "a = 0x_1", 1, "invalid number"},
		{"signed prefix", "a = +0x1", 1, "cannot have a sign"},
		{"double sign", "a = --1", 1, "expected a value"},
		{"integer overflow", "a = 9223372036854775808", 1, "invalid integer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.in))
			if tt.msg == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tt.msg)
			}
			var pe *Error
			if !errors.As(err, &pe) {
				t.Fatalf("error is %T, want *Error", err)
			}
			if pe.Line != tt.line {
				t.Errorf("line = %d, want %d (%v)", pe.Line, tt.line, err)
			}
			if !strings.Contains(pe.Msg, tt.msg) {
				t.Errorf("msg = %q, want it to contain %q", pe.Msg, tt.msg)
			}
			if !strings.HasPrefix(err.Error(), "line ") {
				t.Errorf("Error() = %q, want line prefix", err.Error())
			}
		})
	}
}

func TestAccessors(t *testing.T) {
	doc, err := Parse([]byte(`
[project]
name = "demo"
dependencies = ["a", "b", 3]

[[package]]
name = "x"
[[package]]
name = "y"
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := String(doc, "project", "name"); got != "demo" {
		t.Errorf("String = %q", got)
	}
	if got := Strings(doc, "project", "dependencies"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("Strings = %v", got)
	}
	if got := Tables(doc, "package"); len(got) != 2 || got[1]["name"] != "y" {
		t.Errorf("Tables = %v", got)
	}
	if Table(doc, "missing", "x") != nil || String(doc, "project", "name", "deeper") != "" {
		t.Error("missing lookups should be empty")
	}
}

func TestRealCargoLockSnippet(t *testing.T) {
	in := `# This file is automatically @generated by Cargo.
# It is not intended for manual editing.
version = 4

[[package]]
name = "itoa"
version = "1.0.11"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "49f1f14873335454500d59611f1cf4a4b0f786f9ac11f4312a78e4cf2566695b"

[[package]]
name = "serde_json"
version = "1.0.128"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "6ff5456707a1de34e7e37f2a6fd3d3f808c318259cbd01ab6377795054b483d8"
dependencies = [
 "itoa",
 "memchr",
 "ryu",
 "serde",
]
`
	doc, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	pkgs := Tables(doc, "package")
	if len(pkgs) != 2 || doc["version"] != int64(4) {
		t.Fatalf("doc = %#v", doc)
	}
	if got := Strings(pkgs[1], "dependencies"); len(got) != 4 || got[3] != "serde" {
		t.Errorf("deps = %v", got)
	}
}
