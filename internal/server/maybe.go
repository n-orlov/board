package server

import (
	"bytes"
	"encoding/json"
)

// Maybe is a JSON field that tells three cases apart: left out (Set false), null
// (Set true, Null true) and a value. The generated TaskPatch uses it for every
// field, so a patch can clear a date.
type Maybe[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// UnmarshalJSON records that the field was present, and whether it was null. The
// decoder calls it only for a field that is in the document.
func (m *Maybe[T]) UnmarshalJSON(b []byte) error {
	m.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		m.Null = true
		return nil
	}
	return json.Unmarshal(b, &m.Value)
}

// MarshalJSON writes the value, or null when there is none.
func (m Maybe[T]) MarshalJSON() ([]byte, error) {
	if !m.Set || m.Null {
		return []byte("null"), nil
	}
	return json.Marshal(m.Value)
}
