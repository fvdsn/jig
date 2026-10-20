package jig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// jsonObject is a JSON object with its key order kept, so a document people
// edit by hand comes back in the order they wrote it. Values are
// *jsonObject, []any, string, json.Number, bool, or nil.
type jsonObject struct {
	keys   []string
	values map[string]any
}

func newJSONObject() *jsonObject {
	return &jsonObject{values: map[string]any{}}
}

func (o *jsonObject) get(key string) (any, bool) {
	value, ok := o.values[key]
	return value, ok
}

// set assigns a key, appending it when new.
func (o *jsonObject) set(key string, value any) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *jsonObject) remove(key string) {
	if _, ok := o.values[key]; !ok {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

func (o *jsonObject) empty() bool { return len(o.keys) == 0 }

// getPath walks nested objects.
func (o *jsonObject) getPath(path []string) (any, bool) {
	var current any = o
	for _, key := range path {
		object, ok := current.(*jsonObject)
		if !ok {
			return nil, false
		}
		if current, ok = object.get(key); !ok {
			return nil, false
		}
	}
	return current, true
}

// setPath assigns a nested key, creating the objects on the way; a
// non-object in the way is an error, since it is the user's.
func (o *jsonObject) setPath(path []string, value any) error {
	current := o
	for _, key := range path[:len(path)-1] {
		next, ok := current.get(key)
		if !ok {
			child := newJSONObject()
			current.set(key, child)
			current = child
			continue
		}
		child, ok := next.(*jsonObject)
		if !ok {
			return fmt.Errorf("%s is not an object", strings.Join(path, "."))
		}
		current = child
	}
	current.set(path[len(path)-1], value)
	return nil
}

// removePath deletes a nested key and the containers it leaves empty.
func (o *jsonObject) removePath(path []string) {
	if len(path) == 0 {
		return
	}
	parent, ok := o.getPath(path[:len(path)-1])
	if !ok {
		return
	}
	object, ok := parent.(*jsonObject)
	if !ok {
		return
	}
	object.remove(path[len(path)-1])
	if object.empty() && len(path) > 1 {
		o.removePath(path[:len(path)-1])
	}
}

// decodeJSON parses a document keeping object key order.
func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := decodeJSONValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing content after the JSON document")
	}
	return value, nil
}

func decodeJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			object := newJSONObject()
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				value, err := decodeJSONValue(decoder)
				if err != nil {
					return nil, err
				}
				object.set(key, value)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return object, nil
		case '[':
			list := []any{}
			for decoder.More() {
				value, err := decodeJSONValue(decoder)
				if err != nil {
					return nil, err
				}
				list = append(list, value)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return list, nil
		}
		return nil, fmt.Errorf("unexpected %v", t)
	default:
		return token, nil
	}
}

// encodeJSON renders a value with two-space indentation and a trailing
// newline, the way hand-written configuration files tend to look; indent
// "" renders compactly, for hashing.
func encodeJSON(value any, indent string) []byte {
	var b bytes.Buffer
	writeJSONValue(&b, value, indent, 0)
	if indent != "" {
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func writeJSONValue(b *bytes.Buffer, value any, indent string, depth int) {
	newline := func(d int) {
		if indent == "" {
			return
		}
		b.WriteByte('\n')
		b.WriteString(strings.Repeat(indent, d))
	}
	switch v := value.(type) {
	case *jsonObject:
		if v.empty() {
			b.WriteString("{}")
			return
		}
		b.WriteByte('{')
		for i, key := range v.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			newline(depth + 1)
			b.Write(jsonString(key))
			b.WriteByte(':')
			if indent != "" {
				b.WriteByte(' ')
			}
			writeJSONValue(b, v.values[key], indent, depth+1)
		}
		newline(depth)
		b.WriteByte('}')
	case []any:
		if len(v) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			newline(depth + 1)
			writeJSONValue(b, item, indent, depth+1)
		}
		newline(depth)
		b.WriteByte(']')
	case string:
		b.Write(jsonString(v))
	case json.Number:
		b.WriteString(v.String())
	case bool:
		if v {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case nil:
		b.WriteString("null")
	default:
		// Values built by jig (ints, maps) take the standard encoding.
		data, err := json.Marshal(v)
		if err != nil {
			b.WriteString("null")
			return
		}
		b.Write(data)
	}
}

// jsonString quotes a string without HTML escaping, so URLs and commands
// read as written.
func jsonString(s string) []byte {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(s)
	return bytes.TrimRight(b.Bytes(), "\n")
}

// jsonHash fingerprints a value by its compact encoding.
func jsonHash(value any) string {
	return sha256Hex(encodeJSON(value, ""))
}

// jsonList builds a JSON array of strings.
func jsonList(items []string) []any {
	list := make([]any, 0, len(items))
	for _, item := range items {
		list = append(list, item)
	}
	return list
}

// jsonMap builds an object from a string map with sorted keys.
func jsonMap(m map[string]string) *jsonObject {
	object := newJSONObject()
	for _, key := range sortedKeys(mapToBool(m)) {
		object.set(key, m[key])
	}
	return object
}

func mapToBool(m map[string]string) map[string]bool {
	set := make(map[string]bool, len(m))
	for key := range m {
		set[key] = true
	}
	return set
}
