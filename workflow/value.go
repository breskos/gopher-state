package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

type valueKind uint8

const (
	unset valueKind = iota
	null
	boolean
	number
	text
	quantity
)

// Value is a JSON scalar or a quantity. Its zero value is unset, which is distinct from
// JSON null. Numbers use IEEE 754 binary64.
type Value struct {
	kind valueKind
	flag bool
	num  float64
	str  string
}

// Null returns JSON null.
func Null() Value { return Value{kind: null} }

// Bool returns a boolean value.
func Bool(value bool) Value { return Value{kind: boolean, flag: value} }

// Number returns a numeric value. NaN and infinities cannot be persisted or
// compared by conditions.
func Number(value float64) Value { return Value{kind: number, num: value} }

// String returns a string value.
func String(value string) Value { return Value{kind: text, str: value} }

// Quantity returns a number with a unit. Unit names match exactly.
func Quantity(amount float64, unit string) Value {
	return Value{kind: quantity, num: amount, str: unit}
}

// IsZero reports whether the value is unset.
func (v Value) IsZero() bool { return v.kind == unset }

func (v Value) valid() bool {
	return v.kind != unset && (v.kind != quantity || v.str != "") &&
		(!v.numeric() || (!math.IsNaN(v.num) && !math.IsInf(v.num, 0)))
}

func (v Value) numeric() bool { return v.kind == number || v.kind == quantity }

// MarshalJSON encodes a scalar or a quantity.
func (v Value) MarshalJSON() ([]byte, error) {
	switch v.kind {
	case null:
		return []byte("null"), nil
	case boolean:
		return json.Marshal(v.flag)
	case number:
		return json.Marshal(v.num)
	case text:
		return json.Marshal(v.str)
	case quantity:
		if !v.valid() {
			return nil, fmt.Errorf("quantity requires a finite amount and a nonempty unit")
		}
		return json.Marshal(struct {
			Amount float64 `json:"amount"`
			Unit   string  `json:"unit"`
		}{v.num, v.str})
	default:
		return nil, fmt.Errorf("cannot encode an unset workflow value")
	}
}

// UnmarshalJSON decodes a scalar or an object with an amount and unit.
func (v *Value) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	switch {
	case bytes.Equal(data, []byte("null")):
		*v = Null()
	case bytes.Equal(data, []byte("true")):
		*v = Bool(true)
	case bytes.Equal(data, []byte("false")):
		*v = Bool(false)
	case len(data) != 0 && data[0] == '{':
		var q struct {
			Amount *float64 `json:"amount"`
			Unit   string   `json:"unit"`
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&q); err != nil {
			return err
		}
		if q.Amount == nil || q.Unit == "" {
			return fmt.Errorf("quantity requires an amount and a nonempty unit")
		}
		*v = Quantity(*q.Amount, q.Unit)
	case len(data) != 0 && data[0] == '"':
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*v = String(value)
	default:
		var value float64
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*v = Number(value)
	}
	return nil
}

// Facts maps field names to scalar values or quantities for the current event.
// Treat a map as read-only while an engine is evaluating it.
type Facts map[string]Value
