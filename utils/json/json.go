package json

import "github.com/bytedance/sonic"

// Marshal serializes v using sonic.
func Marshal(v any) ([]byte, error) {
	return sonic.Marshal(v)
}

// MarshalString serializes v into a JSON string using sonic.
func MarshalString(v any) (string, error) {
	return sonic.MarshalString(v)
}

// Unmarshal deserializes JSON bytes into v using sonic.
func Unmarshal(data []byte, v any) error {
	return sonic.Unmarshal(data, v)
}

// UnmarshalString deserializes a JSON string into v using sonic.
func UnmarshalString(data string, v any) error {
	return sonic.UnmarshalString(data, v)
}

// Valid reports whether data is a valid JSON encoding.
func Valid(data []byte) bool {
	return sonic.Valid(data)
}

// ValidString reports whether data is a valid JSON encoding.
func ValidString(data string) bool {
	return sonic.ValidString(data)
}
