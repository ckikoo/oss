package video

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"oss/config"
	"oss/consts"
	"oss/service/do"
)

func TestChooseH264EncoderPrefersHardware(t *testing.T) {
	output := `
 V..... libx264              libx264 H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10
 V....D h264_qsv             H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10 (Intel Quick Sync Video acceleration)
 V....D h264_nvenc           NVIDIA NVENC H.264 encoder
`
	if got := chooseH264Encoder(output); got != "h264_nvenc" {
		t.Fatalf("chooseH264Encoder() = %q, want h264_nvenc", got)
	}
}

func TestBuildFFmpegArgsUsesProfileWidth(t *testing.T) {
	profile := &do.VideoProfileDo{
		Width:        1280,
		Height:       720,
		VideoBitrate: "2000k",
		AudioBitrate: "128k",
	}

	args := buildFFmpegArgsForEncoder("input.mp4", "out", "key.info", profile, "300", cpuH264Encoder)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-vf scale=1280:720") {
		t.Fatalf("ffmpeg args should use profile width, got %q", joined)
	}
}

func TestBuildFFmpegArgsFallsBackToAutoWidth(t *testing.T) {
	profile := &do.VideoProfileDo{
		Height:       480,
		VideoBitrate: "800k",
		AudioBitrate: "96k",
	}

	args := buildFFmpegArgsForEncoder("input.mp4", "out", "key.info", profile, "300", cpuH264Encoder)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-vf scale=-2:480") {
		t.Fatalf("ffmpeg args should keep auto width fallback, got %q", joined)
	}
}

func TestRunFFmpegFallsBackToCPUWhenGPUFails(t *testing.T) {
	outputDir := t.TempDir()
	partialPath := filepath.Join(outputDir, "partial.ts")
	runner := &fakeFFmpegRunner{
		results: []fakeFFmpegResult{
			{output: " V....D h264_nvenc           NVIDIA NVENC H.264 encoder\n"},
			{output: "gpu unavailable", err: errors.New("exit status 1")},
			{},
		},
		onRun: func(call int, args []string) {
			if call == 1 {
				if err := os.WriteFile(partialPath, []byte("partial"), consts.FilePermFile); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			}
		},
	}
	processor := &Processor{ffmpegRunner: runner}
	profile := &do.VideoProfileDo{
		Profile:      consts.VideoProfile720P,
		Height:       720,
		Fps:          30,
		VideoBitrate: "2000k",
		AudioBitrate: "128k",
	}

	if err := processor.runFFmpeg(context.Background(), "input.mp4", outputDir, "key.info", profile); err != nil {
		t.Fatalf("runFFmpeg() error = %v", err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("ffmpeg calls = %d, want 3", len(runner.calls))
	}
	if !hasArgPair(runner.calls[1], "-c:v", "h264_nvenc") {
		t.Fatalf("gpu call missing h264_nvenc: %v", runner.calls[1])
	}
	if !hasArgPair(runner.calls[2], "-c:v", cpuH264Encoder) {
		t.Fatalf("cpu fallback call missing libx264: %v", runner.calls[2])
	}
	if _, err := os.Stat(partialPath); !os.IsNotExist(err) {
		t.Fatalf("partial gpu output should be removed before fallback, stat err = %v", err)
	}
	if got := processor.selectH264Encoder(context.Background()); got != cpuH264Encoder {
		t.Fatalf("selectH264Encoder() after gpu failure = %q, want %s", got, cpuH264Encoder)
	}
}

func TestRunFFmpegUsesCPUWhenNoHardwareEncoder(t *testing.T) {
	probeCount := 0
	for _, encoder := range hardwareH264EncoderPreference {
		probeCount += len(buildH264EncoderProbeArgs(encoder))
	}

	results := make([]fakeFFmpegResult, 0, probeCount+1)
	for i := 0; i < probeCount; i++ {
		results = append(results, fakeFFmpegResult{err: errors.New("encoder unavailable")})
	}
	results = append(results, fakeFFmpegResult{})
	runner := &fakeFFmpegRunner{
		results: results,
	}
	processor := &Processor{ffmpegRunner: runner}
	profile := &do.VideoProfileDo{
		Profile:      consts.VideoProfile480P,
		Height:       480,
		Fps:          30,
		VideoBitrate: "800k",
		AudioBitrate: "96k",
	}

	if err := processor.runFFmpeg(context.Background(), "input.mp4", t.TempDir(), "key.info", profile); err != nil {
		t.Fatalf("runFFmpeg() error = %v", err)
	}
	wantCalls := probeCount + 1
	if len(runner.calls) != wantCalls {
		t.Fatalf("ffmpeg calls = %d, want %d", len(runner.calls), wantCalls)
	}
	transcodeCall := runner.calls[len(runner.calls)-1]
	if !hasArgPair(transcodeCall, "-c:v", cpuH264Encoder) {
		t.Fatalf("transcode call missing libx264: %v", transcodeCall)
	}
}

func TestProfileKeyEncryptDecryptSupportsBase64MasterKey(t *testing.T) {
	masterKey := []byte("12345678901234567890123456789012")
	processor := &Processor{
		security: config.Security{
			AESKey: base64.StdEncoding.EncodeToString(masterKey),
		},
	}

	rawKey := []byte("1234567890123456")
	encrypted, err := processor.encryptProfileKey(rawKey)
	if err != nil {
		t.Fatalf("encryptProfileKey() error = %v", err)
	}
	decrypted, err := processor.decryptProfileKey(&do.VideoEncryptKeyDo{
		KeyID:        "key-1",
		EncryptedKey: encrypted,
	})
	if err != nil {
		t.Fatalf("decryptProfileKey() error = %v", err)
	}
	if string(decrypted.raw) != string(rawKey) {
		t.Fatalf("decrypted key = %q, want %q", string(decrypted.raw), string(rawKey))
	}
}

type fakeFFmpegResult struct {
	output string
	err    error
}

type fakeFFmpegRunner struct {
	results []fakeFFmpegResult
	calls   [][]string
	onRun   func(call int, args []string)
}

func (r *fakeFFmpegRunner) Run(ctx context.Context, args []string) (string, error) {
	call := len(r.calls)
	copiedArgs := append([]string(nil), args...)
	r.calls = append(r.calls, copiedArgs)
	if r.onRun != nil {
		r.onRun(call, copiedArgs)
	}
	if call >= len(r.results) {
		return "", nil
	}
	result := r.results[call]
	return result.output, result.err
}

func hasArgPair(args []string, key string, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestParsePlaylistDurationMs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.m3u8")
	body := "#EXTM3U\n#EXTINF:6.500,\nseg_000001.ts\n#EXTINF:4,\nseg_000002.ts\n"
	if err := os.WriteFile(path, []byte(body), consts.FilePermFile); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	duration, err := parsePlaylistDurationMs(path)
	if err != nil {
		t.Fatalf("parsePlaylistDurationMs() error = %v", err)
	}
	if duration != 10500 {
		t.Fatalf("duration = %d, want 10500", duration)
	}
}

func TestAggregateProfiles(t *testing.T) {
	done, derived, allDone := aggregateProfiles([]*do.VideoProfileDo{
		{Status: consts.TranscodeStatusDone, Size: 10},
		{Status: consts.TranscodeStatusProcessing, Size: 5},
		{Status: consts.TranscodeStatusDeleted, Size: 100},
	})
	if done != 1 || derived != 15 || allDone {
		t.Fatalf("aggregate = (%d, %d, %v), want (1, 15, false)", done, derived, allDone)
	}
}

func TestTailBufferKeepsLastBytes(t *testing.T) {
	buf := &tailBuffer{limit: 5}
	if _, err := buf.Write([]byte("abcdef")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := buf.Write([]byte("gh")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "defgh") || !strings.HasPrefix(got, "[truncated]") {
		t.Fatalf("tail buffer = %q, want truncated defgh", got)
	}
}
