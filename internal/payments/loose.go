package payments

import (
	"bytes"
	"encoding/json"
	"strings"
)

// looseValue is any JSON scalar read leniently: the API sends flags and numbers both as native
// values and as strings.
type looseValue struct {
	raw json.RawMessage
}

func (v *looseValue) UnmarshalJSON(b []byte) error {
	v.raw = append(v.raw[:0], b...)
	return nil
}

// str is the scalar as text ("" for absent, null or a non-scalar).
func (v looseValue) str() string {
	b := bytes.TrimSpace(v.raw)
	if len(b) == 0 || string(b) == "null" {
		return ""
	}
	if b[0] == '"' {
		var s string
		if json.Unmarshal(b, &s) != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}
	if b[0] == '{' || b[0] == '[' {
		return ""
	}
	return string(b)
}

// compound is true for a present object or array: a value, but not a scalar one.
func (v looseValue) compound() bool {
	b := bytes.TrimSpace(v.raw)
	return len(b) > 0 && (b[0] == '{' || b[0] == '[')
}

// failed is true only for an explicit false (bool or string).
func (v looseValue) failed() bool {
	return strings.EqualFold(v.str(), "false")
}
