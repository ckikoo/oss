package dto

import "time"

type HealthCheckItem struct {
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type HealthResp struct {
	Status    string                     `json:"status"`
	Timestamp time.Time                  `json:"timestamp"`
	Checks    map[string]HealthCheckItem `json:"checks"`
}
