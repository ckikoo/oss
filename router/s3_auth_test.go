package router

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

func TestCanonicalQueryWithEscaperSortsKeysAndValues(t *testing.T) {
	got := canonicalQueryWithEscaper("z=last&a=two&a=one&space=a+b", s3URIEncode)
	want := "a=one&a=two&space=a%20b&z=last"
	if got != want {
		t.Fatalf("canonical query = %q, want %q", got, want)
	}
}

func TestCanonicalS3QueryKeepsEmptySubresource(t *testing.T) {
	got := canonicalS3Query("uploads=&partNumber=2&uploadId=abc")
	want := "partNumber=2&uploadId=abc&uploads="
	if got != want {
		t.Fatalf("canonical S3 query = %q, want %q", got, want)
	}
}

func TestS3URIEncodePathPreservesSeparators(t *testing.T) {
	got := s3URIEncodePath("/bucket/a b/中.txt")
	want := "/bucket/a%20b/%E4%B8%AD.txt"
	if got != want {
		t.Fatalf("encoded path = %q, want %q", got, want)
	}
}

func TestParseS3Authorization(t *testing.T) {
	header := "AWS4-HMAC-SHA256 Credential=AKID/20260524/us-east-1/s3/aws4_request, SignedHeaders=X-Amz-Date;host;x-amz-content-sha256, Signature=abcdef"

	info, err := parseS3Authorization(header)
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	if info.AccessKey != "AKID" || info.Date != "20260524" || info.Region != "us-east-1" || info.Service != "s3" || info.Signature != "abcdef" {
		t.Fatalf("unexpected auth info: %+v", info)
	}
	gotHeaders := strings.Join(info.SignedHeaders, ";")
	wantHeaders := "host;x-amz-content-sha256;x-amz-date"
	if gotHeaders != wantHeaders {
		t.Fatalf("signed headers = %q, want %q", gotHeaders, wantHeaders)
	}
}

func TestParseS3AuthorizationRejectsInvalidScope(t *testing.T) {
	_, err := parseS3Authorization("AWS4-HMAC-SHA256 Credential=AKID/20260524/us-east-1/s3/not_aws, SignedHeaders=host, Signature=abcdef")
	if err == nil {
		t.Fatal("expected invalid credential scope error")
	}
}

func TestBuildS3CanonicalRequest(t *testing.T) {
	var c app.RequestContext
	c.Request.Header.SetMethod("GET")
	c.Request.SetRequestURI("/bucket/a b.txt?z=last&a=one")
	c.Request.Header.SetHost("example.com")
	c.Request.Header.Set("x-amz-date", "20260524T010203Z")
	c.Request.Header.Set("x-amz-content-sha256", "UNSIGNED-PAYLOAD")

	got, err := buildS3CanonicalRequest(&c, []string{"host", "x-amz-content-sha256", "x-amz-date"})
	if err != nil {
		t.Fatalf("build canonical request returned error: %v", err)
	}

	want := "GET\n" +
		"/bucket/a%20b.txt\n" +
		"a=one&z=last\n" +
		"host:example.com\n" +
		"x-amz-content-sha256:UNSIGNED-PAYLOAD\n" +
		"x-amz-date:20260524T010203Z\n" +
		"\n" +
		"host;x-amz-content-sha256;x-amz-date\n" +
		"UNSIGNED-PAYLOAD"
	if got != want {
		t.Fatalf("canonical request mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildS3CanonicalRequestRequiresSignedHeaders(t *testing.T) {
	var c app.RequestContext
	c.Request.Header.SetMethod("GET")
	c.Request.SetRequestURI("/")
	c.Request.Header.SetHost("example.com")

	_, err := buildS3CanonicalRequest(&c, []string{"host", "x-amz-date"})
	if err == nil {
		t.Fatal("expected missing signed header error")
	}
}

func TestS3SigningKeyMatchesAWSDerivationExample(t *testing.T) {
	got := hex.EncodeToString(s3SigningKey("wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "20150830", "us-east-1", "iam"))
	want := "c4afb1cc5771d871763a393e44b70357" +
		"1b55cc28424d1a5e86da6ed3c154a4b9"
	if got != want {
		t.Fatalf("signing key = %q, want %q", got, want)
	}
}

func TestSha256Hex(t *testing.T) {
	sum := sha256.Sum256([]byte("hello"))
	want := hex.EncodeToString(sum[:])
	if got := sha256Hex("hello"); got != want {
		t.Fatalf("sha256Hex = %q, want %q", got, want)
	}
}

func TestValidS3RequestTime(t *testing.T) {
	now := time.Now().UTC().Format("20060102T150405Z")
	if !validS3RequestTime(now, time.Minute) {
		t.Fatal("current timestamp should be valid")
	}
	if validS3RequestTime("not-a-date", time.Minute) {
		t.Fatal("invalid timestamp should be rejected")
	}
}
