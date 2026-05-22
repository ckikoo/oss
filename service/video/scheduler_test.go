package video

import (
	"testing"

	"oss/consts"
)

func TestParseVideoMeta(t *testing.T) {
	meta, err := parseVideoMeta([]byte(`{
		"streams": [
			{"width": 1920, "height": 1080, "r_frame_rate": "30000/1001"}
		]
	}`))
	if err != nil {
		t.Fatalf("parseVideoMeta() error = %v", err)
	}
	if meta.width != 1920 || meta.height != 1080 || meta.fps != 30 {
		t.Fatalf("meta = %+v, want 1920x1080@30", meta)
	}
}

func TestBuildDefaultProfileCreatesSetsDimensions(t *testing.T) {
	profiles := buildDefaultProfileCreates([]consts.VideoTranscodeProfile{
		{Profile: consts.VideoProfileOriginal, Height: 0},
		{Profile: consts.VideoProfile720P, Height: 720, VideoBitrate: "2000k", AudioBitrate: "128k"},
	}, &videoMeta{width: 1920, height: 1080, fps: 30})

	if len(profiles) != 2 {
		t.Fatalf("len(profiles) = %d, want 2", len(profiles))
	}
	if profiles[0].Width != 1920 || profiles[0].Height != 1080 {
		t.Fatalf("original dimensions = %dx%d, want 1920x1080", profiles[0].Width, profiles[0].Height)
	}
	if profiles[1].Width != 1280 || profiles[1].Height != 720 {
		t.Fatalf("720p dimensions = %dx%d, want 1280x720", profiles[1].Width, profiles[1].Height)
	}
}

func TestTargetProfileDimensionsRoundsWidthToEven(t *testing.T) {
	width, height := targetProfileDimensions(1080, 1920, 720)
	if width != 406 || height != 720 {
		t.Fatalf("dimensions = %dx%d, want 406x720", width, height)
	}
}
