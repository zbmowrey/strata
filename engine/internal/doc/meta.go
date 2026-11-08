package doc

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// Meta is decoded frontmatter: an ordered top-level mapping with source
// positions for each key.
type Meta struct {
	vals map[string]any
	pos  map[string][2]int
	keys []string
}

// decodeMeta decodes YAML frontmatter; lineBase is added to node lines so
// positions refer to the file.
func decodeMeta(src []byte, lineBase int) (*Meta, error) {
	m := &Meta{vals: map[string]any{}, pos: map[string][2]int{}}
	if strings.TrimSpace(string(src)) == "" {
		return m, nil
	}
	f, err := parser.ParseBytes(src, 0)
	if err != nil {
		return nil, yamlPosError(err, lineBase)
	}
	if len(f.Docs) == 0 || f.Docs[0] == nil || f.Docs[0].Body == nil {
		return m, nil
	}
	body := f.Docs[0].Body
	if c, ok := body.(*ast.CommentNode); ok && c != nil {
		return m, nil
	}
	mapping, ok := asMapping(body)
	if !ok {
		tk := body.GetToken()
		return nil, &PosError{Err: fmt.Errorf("%w: frontmatter must be a mapping", ErrFrontmatter), Line: tk.Position.Line + lineBase, Col: tk.Position.Column}
	}
	for _, mv := range mapping {
		key := mv.Key.GetToken().Value
		var v any
		if err := yaml.NodeToValue(mv.Value, &v); err != nil {
			return nil, yamlPosError(err, lineBase)
		}
		if _, dup := m.vals[key]; dup {
			p := mv.Key.GetToken().Position
			return nil, &PosError{Err: fmt.Errorf("%w: duplicate key %q", ErrFrontmatter, key), Line: p.Line + lineBase, Col: p.Column}
		}
		p := mv.Key.GetToken().Position
		m.keys = append(m.keys, key)
		m.vals[key] = v
		m.pos[key] = [2]int{p.Line + lineBase, p.Column}
	}
	return m, nil
}

// asMapping returns the key/value pairs of a mapping node.
func asMapping(n ast.Node) ([]*ast.MappingValueNode, bool) {
	switch v := n.(type) {
	case *ast.MappingNode:
		return v.Values, true
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{v}, true
	default:
		return nil, false
	}
}

// yamlPosError converts a goccy error into a PosError when it carries a token.
func yamlPosError(err error, lineBase int) error {
	var se *yaml.SyntaxError
	if errors.As(err, &se) && se.Token != nil {
		return &PosError{Err: fmt.Errorf("%w: %s", ErrFrontmatter, se.Message), Line: se.Token.Position.Line + lineBase, Col: se.Token.Position.Column}
	}
	var te *yaml.TypeError
	if errors.As(err, &te) && te.Token != nil {
		return &PosError{Err: fmt.Errorf("%w: %w", ErrFrontmatter, err), Line: te.Token.Position.Line + lineBase, Col: te.Token.Position.Column}
	}
	return &PosError{Err: fmt.Errorf("%w: %w", ErrFrontmatter, err), Line: 1 + lineBase, Col: 1}
}

// Keys returns the top-level keys in source order.
func (m *Meta) Keys() []string { return append([]string(nil), m.keys...) }

// Len returns the number of keys.
func (m *Meta) Len() int { return len(m.keys) }

// Has reports whether key is present (even with a null value).
func (m *Meta) Has(key string) bool { _, ok := m.vals[key]; return ok }

// Get returns the raw decoded value.
func (m *Meta) Get(key string) (any, bool) { v, ok := m.vals[key]; return v, ok }

// Pos returns the 1-based file line and column of key, or (0,0).
func (m *Meta) Pos(key string) (line, col int) { p := m.pos[key]; return p[0], p[1] }

// String returns a scalar value as a string ("" when absent or null).
func (m *Meta) String(key string) string {
	v, ok := m.vals[key]
	if !ok || v == nil {
		return ""
	}
	return Scalar(v)
}

// Scalar formats a decoded scalar value as text.
func Scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case uint64:
		return strconv.FormatUint(x, 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

// Strings returns a list value (or a single scalar) as strings.
func (m *Meta) Strings(key string) []string {
	v, ok := m.vals[key]
	if !ok || v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return []string{Scalar(v)}
	}
	out := make([]string, 0, len(list))
	for _, x := range list {
		out = append(out, Scalar(x))
	}
	return out
}

// Map returns a copy of all values keyed by name.
func (m *Meta) Map() map[string]any {
	out := make(map[string]any, len(m.vals))
	for k, v := range m.vals {
		out[k] = v
	}
	return out
}

// SortedKeys returns the keys sorted lexically (for deterministic output).
func (m *Meta) SortedKeys() []string {
	k := m.Keys()
	sort.Strings(k)
	return k
}

// ParseYAML decodes a standalone YAML mapping (e.g. script.yaml) into Meta.
func ParseYAML(src []byte) (*Meta, error) { return decodeMeta(src, 0) }
