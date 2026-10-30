package canon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// MarshalPython emits JSON with Python json.dumps defaults used by §7.3.2:
// sort_keys=True, ensure_ascii=False, separators (", ", ": ").
func MarshalPython(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writePython(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writePython(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
		return nil
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	case json.Number:
		s := x.String()
		if s == "" {
			return fmt.Errorf("empty json.Number")
		}
		buf.WriteString(s)
		return nil
	case float64:
		// Prefer integer form when exact; otherwise strconv matching Go's default
		// for finite floats is close enough for vectors that use json.Number.
		buf.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
		return nil
	case float32:
		buf.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
		return nil
	case int:
		buf.WriteString(strconv.Itoa(x))
		return nil
	case int64:
		buf.WriteString(strconv.FormatInt(x, 10))
		return nil
	case int32:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
		return nil
	case uint64:
		buf.WriteString(strconv.FormatUint(x, 10))
		return nil
	case string:
		b, err := json.Marshal(x)
		if err != nil {
			return err
		}
		buf.Write(b)
		return nil
	case []any:
		buf.WriteByte('[')
		for i, el := range x {
			if i > 0 {
				buf.WriteString(", ")
			}
			if err := writePython(buf, el); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteString(", ")
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteString(": ")
			if err := writePython(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case json.RawMessage:
		var decoded any
		dec := json.NewDecoder(bytes.NewReader(x))
		dec.UseNumber()
		if err := dec.Decode(&decoded); err != nil {
			return err
		}
		return writePython(buf, decoded)
	default:
		// Fallback: round-trip through encoding/json with UseNumber where possible.
		b, err := json.Marshal(x)
		if err != nil {
			return err
		}
		var decoded any
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&decoded); err != nil {
			return err
		}
		return writePython(buf, decoded)
	}
}

// DecodeUseNumber parses JSON keeping number tokens as json.Number.
func DecodeUseNumber(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}
