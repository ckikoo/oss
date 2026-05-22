package json

import "testing"

type samplePayload struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestMarshalAndUnmarshal(t *testing.T) {
	data, err := Marshal(samplePayload{Name: "video", Count: 2})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !Valid(data) {
		t.Fatalf("Marshal() produced invalid JSON: %s", string(data))
	}

	var got samplePayload
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.Name != "video" || got.Count != 2 {
		t.Fatalf("Unmarshal() = %+v", got)
	}
}

func TestMarshalStringAndUnmarshalString(t *testing.T) {
	data, err := MarshalString(samplePayload{Name: "cache", Count: 3})
	if err != nil {
		t.Fatalf("MarshalString() error = %v", err)
	}
	if !ValidString(data) {
		t.Fatalf("MarshalString() produced invalid JSON: %s", data)
	}

	var got samplePayload
	if err := UnmarshalString(data, &got); err != nil {
		t.Fatalf("UnmarshalString() error = %v", err)
	}
	if got.Name != "cache" || got.Count != 3 {
		t.Fatalf("UnmarshalString() = %+v", got)
	}
}
