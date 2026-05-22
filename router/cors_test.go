package router

import (
	"testing"

	"oss/config"

	"github.com/cloudwego/hertz/pkg/app"
)

func TestSetVideoPlaybackFallbackCORSHeadersDefaultsToWildcard(t *testing.T) {
	var c app.RequestContext
	c.Request.Header.Set("Origin", "https://player.example.com")

	setVideoPlaybackFallbackCORSHeaders(&c, config.CORS{})

	if got := string(c.Response.Header.Peek("Access-Control-Allow-Origin")); got != "*" {
		t.Fatalf("allow origin = %q, want *", got)
	}
	if got := string(c.Response.Header.Peek("Access-Control-Allow-Methods")); got != "GET, HEAD, OPTIONS" {
		t.Fatalf("allow methods = %q, want GET, HEAD, OPTIONS", got)
	}
	if got := string(c.Response.Header.Peek("Access-Control-Allow-Headers")); got != defaultCORSHeaders {
		t.Fatalf("allow headers = %q, want %q", got, defaultCORSHeaders)
	}
}

func TestSetVideoPlaybackFallbackCORSHeadersUsesGlobalOrigin(t *testing.T) {
	var c app.RequestContext
	c.Request.Header.Set("Origin", "https://player.example.com")

	setVideoPlaybackFallbackCORSHeaders(&c, config.CORS{
		AllowedOrigins: []string{"https://player.example.com"},
	})

	if got := string(c.Response.Header.Peek("Access-Control-Allow-Origin")); got != "https://player.example.com" {
		t.Fatalf("allow origin = %q, want https://player.example.com", got)
	}
}
