package fsutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

// Object is a JSON object that remembers key order, so tools can edit
// files such as .claude/settings.json without reordering the user's keys.
type Object struct {
	vals map[string]json.RawMessage
	keys []string
}

// NewObject returns an empty object.
func NewObject() *Object { return &Object{vals: map[string]json.RawMessage{}} }

// ParseObject parses a JSON object, preserving key order. Empty input
// yields an empty object.
func ParseObject(raw []byte) (*Object, error) {
	o := NewObject()
	if len(bytes.TrimSpace(raw)) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("parse json: top level is not an object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("parse json: %w", err)
		}
		key, _ := kt.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("parse json key %q: %w", key, err)
		}
		o.Set(key, v)
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("parse json: trailing data after object")
	}
	return o, nil
}

// Keys returns the keys in order.
func (o *Object) Keys() []string { return slices.Clone(o.keys) }

// Get returns the raw value of key.
func (o *Object) Get(key string) (json.RawMessage, bool) { v, ok := o.vals[key]; return v, ok }

// Set sets key to a raw JSON value, appending new keys at the end.
func (o *Object) Set(key string, v json.RawMessage) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

// SetValue marshals v and sets it.
func (o *Object) SetValue(key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", key, err)
	}
	o.Set(key, b)
	return nil
}

// Delete removes key.
func (o *Object) Delete(key string) {
	if _, ok := o.vals[key]; !ok {
		return
	}
	delete(o.vals, key)
	o.keys = slices.DeleteFunc(o.keys, func(k string) bool { return k == key })
}

// Object returns the nested object at key (empty when absent).
func (o *Object) Object(key string) (*Object, error) {
	v, ok := o.vals[key]
	if !ok {
		return NewObject(), nil
	}
	return ParseObject(v)
}

// SetObject stores a nested object.
func (o *Object) SetObject(key string, child *Object) error {
	b, err := child.MarshalJSON()
	if err != nil {
		return err
	}
	o.Set(key, b)
	return nil
}

// MarshalJSON renders the object compactly in key order.
func (o *Object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, fmt.Errorf("marshal key: %w", err)
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Pretty renders the object with two-space indentation and a final newline.
func (o *Object) Pretty() ([]byte, error) {
	raw, err := o.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, raw, "", "  "); err != nil {
		return nil, fmt.Errorf("indent json: %w", err)
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}
