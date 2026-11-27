package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// ReadJSON reads and compiles one workflow document. The caller supplies a
// positive byte limit. Unknown fields outside Metadata are rejected.
func ReadJSON(r io.Reader, maxBytes int64) (*Engine, error) {
	var def Definition
	if err := readDocument(r, maxBytes, &def); err != nil {
		return nil, err
	}
	return Compile(def)
}

func readDocument(r io.Reader, maxBytes int64, target any) error {
	if maxBytes <= 0 {
		return fmt.Errorf("%w: document byte limit must be positive", ErrInvalidDefinition)
	}
	limited := &io.LimitedReader{R: r, N: maxBytes}
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read workflow: %w", err)
	}
	if limited.N == 0 {
		var extra [1]byte
		if _, err := io.ReadFull(r, extra[:]); err != io.EOF {
			if err != nil {
				return fmt.Errorf("read workflow: %w", err)
			}
			return fmt.Errorf("%w: document exceeds %d bytes", ErrInvalidDefinition, maxBytes)
		}
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("%w: document is not UTF-8", ErrInvalidDefinition)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidDefinition, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing content", ErrInvalidDefinition)
	}
	return nil
}
