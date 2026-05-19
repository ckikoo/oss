package config

import (
	"oss/consts"
	"testing"
)

func TestVideoDefaults(t *testing.T) {
	v := Video{}

	if got := v.GetTranscodeMaxConcurrency(); got != consts.DefaultTranscodeMaxConcurrency {
		t.Fatalf("GetTranscodeMaxConcurrency() = %d, want %d", got, consts.DefaultTranscodeMaxConcurrency)
	}
	if got := v.GetSegmentDurationSeconds(); got != consts.HLSSegmentDurationSeconds {
		t.Fatalf("GetSegmentDurationSeconds() = %d, want %d", got, consts.HLSSegmentDurationSeconds)
	}
	if got := v.GetPlayTokenTTLSeconds(); got != consts.DefaultPlayTokenTTLSeconds {
		t.Fatalf("GetPlayTokenTTLSeconds() = %d, want %d", got, consts.DefaultPlayTokenTTLSeconds)
	}
}

func TestVideoConfiguredValues(t *testing.T) {
	v := Video{
		TranscodeMaxConcurrency: 3,
		SegmentDurationSeconds:  6,
		PlayTokenTTLSeconds:     60,
	}

	if got := v.GetTranscodeMaxConcurrency(); got != 3 {
		t.Fatalf("GetTranscodeMaxConcurrency() = %d, want 3", got)
	}
	if got := v.GetSegmentDurationSeconds(); got != 6 {
		t.Fatalf("GetSegmentDurationSeconds() = %d, want 6", got)
	}
	if got := v.GetPlayTokenTTLSeconds(); got != 60 {
		t.Fatalf("GetPlayTokenTTLSeconds() = %d, want 60", got)
	}
}
