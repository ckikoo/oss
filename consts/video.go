package consts

import (
	"path"
	"strings"
)

const (
	TaskBizTypeVideoProfile = "video_profile"
)

const (
	TranscodeStatusPending    int32 = 0
	TranscodeStatusProcessing int32 = 1
	TranscodeStatusDone       int32 = 2
	TranscodeStatusFailed     int32 = 3
	TranscodeStatusDeleted    int32 = 4
)

const (
	PlayVideoAction          = "PlayVideo"
	GetTranscodeStatusAction = "GetTranscodeStatus"
)

const (
	HeaderPlayToken = "X-Play-Token"
)

const (
	HLSAssetPrefix                 = "_video"
	HLSEncryptionAlgorithm         = "HLS-AES-128"
	HLSSegmentDurationSeconds      = 10
	DefaultPlayTokenTTLSeconds     = 14400
	DefaultTranscodeMaxConcurrency = 1
)

const (
	VideoProfileOriginal = "original" // 原画，不重新编码
	VideoProfile2160P    = "2160p"
	VideoProfile1080P    = "1080p"
	VideoProfile720P     = "720p"
	VideoProfile480P     = "480p"
	VideoProfile360P     = "360p"
)

type VideoTranscodeProfile struct {
	Profile      string
	Height       int32
	Fps          int32
	VideoBitrate string
	AudioBitrate string
}

var StandardHeights = []int{360, 480, 720, 1080, 2160}

var defaultVideoTranscodeProfiles = []VideoTranscodeProfile{
	{Profile: VideoProfileOriginal, Height: 0, VideoBitrate: "", AudioBitrate: ""}, // Height=0 代表原画
	{Profile: VideoProfile2160P, Height: 2160, VideoBitrate: "16000k", AudioBitrate: "192k"},
	{Profile: VideoProfile1080P, Height: 1080, VideoBitrate: "4000k", AudioBitrate: "128k"},
	{Profile: VideoProfile720P, Height: 720, VideoBitrate: "2000k", AudioBitrate: "128k"},
	{Profile: VideoProfile480P, Height: 480, VideoBitrate: "800k", AudioBitrate: "96k"},
	{Profile: VideoProfile360P, Height: 360, VideoBitrate: "400k", AudioBitrate: "64k"},
}

func DefaultVideoTranscodeProfiles() []VideoTranscodeProfile {
	profiles := make([]VideoTranscodeProfile, len(defaultVideoTranscodeProfiles))
	copy(profiles, defaultVideoTranscodeProfiles)
	return profiles
}

func ValidVideoContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	switch contentType {
	case "video/mp4", "video/quicktime", "video/x-matroska", "video/x-msvideo":
		return true
	default:
		return false
	}
}

func ValidVideoExtension(extension string) bool {
	extension = strings.ToLower(strings.TrimSpace(extension))
	if extension == "" {
		return false
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}

	switch extension {
	case ".mp4", ".mov", ".mkv", ".avi":
		return true
	default:
		return false
	}
}

func IsVideoObject(contentType string, objectKey string) bool {
	return ValidVideoContentType(contentType) || ValidVideoExtension(path.Ext(objectKey))
}
