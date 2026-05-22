package video

import (
	"sync"
	"sync/atomic"
)

// videoMetrics keeps lightweight in-process counters for the video pipeline.
// It intentionally has no external dependency; production deployments can wire
// these snapshots into Prometheus/OpenTelemetry exporters later without
// touching the business path.
type videoMetrics struct {
	transcodeTotalMu sync.Mutex
	transcodeTotal   map[string]int64

	transcodeDurationMu sync.Mutex
	transcodeDuration   map[string][]float64

	transcodeDerivedBytesMu sync.Mutex
	transcodeDerivedBytes   map[string]int64

	playTokenTotalMu sync.Mutex
	playTokenTotal   map[string]int64

	keyRequestTotalMu sync.Mutex
	keyRequestTotal   map[string]int64

	segmentRequestBytes atomic.Int64
}

type VideoMetricsSnapshot struct {
	TranscodeTotal        map[string]int64     `json:"video_transcode_total"`
	TranscodeDurationSec  map[string][]float64 `json:"video_transcode_duration_seconds"`
	TranscodeDerivedBytes map[string]int64     `json:"video_transcode_derived_bytes"`
	PlayTokenTotal        map[string]int64     `json:"video_play_token_total"`
	KeyRequestTotal       map[string]int64     `json:"video_key_request_total"`
	SegmentRequestBytes   int64                `json:"video_segment_request_bytes"`
}

var defaultVideoMetrics = &videoMetrics{
	transcodeTotal:        make(map[string]int64),
	transcodeDuration:     make(map[string][]float64),
	transcodeDerivedBytes: make(map[string]int64),
	playTokenTotal:        make(map[string]int64),
	keyRequestTotal:       make(map[string]int64),
}

func RecordVideoTranscode(status string, profile string, durationMs int64, derivedBytes int64) {
	defaultVideoMetrics.recordTranscode(status, profile, durationMs, derivedBytes)
}

func RecordVideoPlayToken(result string) {
	defaultVideoMetrics.recordPlayToken(result)
}

func RecordVideoKeyRequest(result string) {
	defaultVideoMetrics.recordKeyRequest(result)
}

func RecordVideoSegmentBytes(bytes int64) {
	defaultVideoMetrics.recordSegmentBytes(bytes)
}

func SnapshotVideoMetrics() VideoMetricsSnapshot {
	return defaultVideoMetrics.snapshot()
}

func (m *videoMetrics) recordTranscode(status string, profile string, durationMs int64, derivedBytes int64) {
	key := labelKey(status, profile)
	m.transcodeTotalMu.Lock()
	m.transcodeTotal[key]++
	m.transcodeTotalMu.Unlock()

	if durationMs > 0 {
		m.transcodeDurationMu.Lock()
		m.transcodeDuration[profile] = append(m.transcodeDuration[profile], float64(durationMs)/1000.0)
		m.transcodeDurationMu.Unlock()
	}

	if derivedBytes > 0 {
		m.transcodeDerivedBytesMu.Lock()
		m.transcodeDerivedBytes[profile] += derivedBytes
		m.transcodeDerivedBytesMu.Unlock()
	}
}

func (m *videoMetrics) recordPlayToken(result string) {
	m.playTokenTotalMu.Lock()
	m.playTokenTotal[result]++
	m.playTokenTotalMu.Unlock()
}

func (m *videoMetrics) recordKeyRequest(result string) {
	m.keyRequestTotalMu.Lock()
	m.keyRequestTotal[result]++
	m.keyRequestTotalMu.Unlock()
}

func (m *videoMetrics) recordSegmentBytes(bytes int64) {
	if bytes > 0 {
		m.segmentRequestBytes.Add(bytes)
	}
}

func (m *videoMetrics) snapshot() VideoMetricsSnapshot {
	m.transcodeTotalMu.Lock()
	transcodeTotal := copyIntMap(m.transcodeTotal)
	m.transcodeTotalMu.Unlock()

	m.transcodeDurationMu.Lock()
	transcodeDuration := copyFloatSliceMap(m.transcodeDuration)
	m.transcodeDurationMu.Unlock()

	m.transcodeDerivedBytesMu.Lock()
	transcodeDerivedBytes := copyIntMap(m.transcodeDerivedBytes)
	m.transcodeDerivedBytesMu.Unlock()

	m.playTokenTotalMu.Lock()
	playTokenTotal := copyIntMap(m.playTokenTotal)
	m.playTokenTotalMu.Unlock()

	m.keyRequestTotalMu.Lock()
	keyRequestTotal := copyIntMap(m.keyRequestTotal)
	m.keyRequestTotalMu.Unlock()

	return VideoMetricsSnapshot{
		TranscodeTotal:        transcodeTotal,
		TranscodeDurationSec:  transcodeDuration,
		TranscodeDerivedBytes: transcodeDerivedBytes,
		PlayTokenTotal:        playTokenTotal,
		KeyRequestTotal:       keyRequestTotal,
		SegmentRequestBytes:   m.segmentRequestBytes.Load(),
	}
}

func transcodeStatusLabel(status int32) string {
	switch status {
	case 0:
		return "pending"
	case 1:
		return "processing"
	case 2:
		return "done"
	case 3:
		return "failed"
	case 4:
		return "deleted"
	default:
		return "unknown"
	}
}

func labelKey(status string, profile string) string {
	if profile == "" {
		profile = "unknown"
	}
	if status == "" {
		status = "unknown"
	}
	return "status=" + status + ",profile=" + profile
}

func copyIntMap(src map[string]int64) map[string]int64 {
	dst := make(map[string]int64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyFloatSliceMap(src map[string][]float64) map[string][]float64 {
	dst := make(map[string][]float64, len(src))
	for k, v := range src {
		dst[k] = append([]float64(nil), v...)
	}
	return dst
}
