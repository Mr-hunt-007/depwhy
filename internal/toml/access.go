package toml

// Lookup follows a path of keys through nested tables.
func Lookup(doc map[string]any, keys ...string) (any, bool) {
	var cur any = doc
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// Table returns the table at the key path, or nil.
func Table(doc map[string]any, keys ...string) map[string]any {
	v, _ := Lookup(doc, keys...)
	m, _ := v.(map[string]any)
	return m
}

// String returns the string at the key path, or "".
func String(doc map[string]any, keys ...string) string {
	v, _ := Lookup(doc, keys...)
	s, _ := v.(string)
	return s
}

// Array returns the array at the key path, or nil.
func Array(doc map[string]any, keys ...string) []any {
	v, _ := Lookup(doc, keys...)
	a, _ := v.([]any)
	return a
}

// Tables returns the elements of an array that are tables.
func Tables(doc map[string]any, keys ...string) []map[string]any {
	var out []map[string]any
	for _, e := range Array(doc, keys...) {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// Strings returns the string elements of an array.
func Strings(doc map[string]any, keys ...string) []string {
	var out []string
	for _, e := range Array(doc, keys...) {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
