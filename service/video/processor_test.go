package video

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"oss/config"
	"oss/consts"
	"oss/service/do"
)

func TestBuildFFmpegArgs(t *testing.T) {
	profile := &do.VideoProfileDo{
		Height:       480,
		VideoBitrate: "800k",
		AudioBitrate: "96k",
	}
	args := buildFFmpegArgs("input.mp4", "out", "key.info", profile, 6)
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"-i input.mp4",
		"-hls_key_info_file key.info",
		"-hls_time 6",
		"-hls_playlist_type vod",
		"-vf scale=-2:480",
		"-b:v 800k",
		"-b:a 96k",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("ffmpeg args missing %q in %q", want, joined)
		}
	}
	if !strings.Contains(args[len(args)-1], hlsPlaylistName) {
		t.Fatalf("last arg should be playlist path, got %q", args[len(args)-1])
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
