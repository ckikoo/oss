package consts

import "testing"

func TestIsVideoObject(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		objectKey   string
		want        bool
	}{
		{name: "mp4 content type", contentType: "video/mp4", objectKey: "movie.bin", want: true},
		{name: "content type with charset", contentType: "video/mp4; charset=utf-8", objectKey: "movie.bin", want: true},
		{name: "uppercase extension", contentType: "application/octet-stream", objectKey: "movie.MP4", want: true},
		{name: "nested mov extension", contentType: "", objectKey: "dir/movie.mov", want: true},
		{name: "non video", contentType: "image/png", objectKey: "image.png", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsVideoObject(tt.contentType, tt.objectKey); got != tt.want {
				t.Fatalf("IsVideoObject() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultVideoTranscodeProfilesReturnsCopy(t *testing.T) {
	profiles := DefaultVideoTranscodeProfiles()
	if len(profiles) != 4 {
		t.Fatalf("len(DefaultVideoTranscodeProfiles()) = %d, want 4", len(profiles))
	}

	profiles[0].Profile = "changed"
	next := DefaultVideoTranscodeProfiles()
	if next[0].Profile != VideoProfile1080P {
		t.Fatalf("DefaultVideoTranscodeProfiles() returned mutable backing slice")
	}
}
