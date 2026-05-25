package router

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"oss/adaptor"
	akGorm "oss/adaptor/repo/accesskey/gorm"
	"oss/common"
	"oss/utils/tools"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

const s3Algorithm = "AWS4-HMAC-SHA256"

type s3AuthInfo struct {
	AccessKey     string
	Date          string
	Region        string
	Service       string
	SignedHeaders []string
	Signature     string
}

func NewS3SignatureV4Middleware(adaptor adaptor.IAdaptor) app.HandlerFunc {
	repo := akGorm.NewAccessKeyRepo(adaptor)
	return func(ctx context.Context, c *app.RequestContext) {
		if string(c.Method()) == "OPTIONS" {
			c.Next(ctx)
			return
		}

		info, err := parseS3Authorization(string(c.GetHeader("Authorization")))
		if err != nil {
			common.WriteS3Error(c, common.AuthErr.WithMsg(err.Error()), string(c.Path()))
			c.Abort()
			return
		}

		amzDate := strings.TrimSpace(string(c.GetHeader("x-amz-date")))
if !validS3RequestTime(amzDate, adaptor.GetConfig().Security.GetS3ReplayWindow()) {
			common.WriteS3Error(c, common.AuthErr.WithMsg("request expired"), string(c.Path()))
			c.Abort()
			return
		}

		ak, sk, err := lookupAndDecryptSK(ctx, repo, adaptor.GetConfig().Security.AESKey, info.AccessKey)
		if err != nil {
			common.WriteS3Error(c, common.AuthErr.WithMsg("invalid access key"), string(c.Path()))
			c.Abort()
			return
		}

		canonicalRequest, err := buildS3CanonicalRequest(c, info.SignedHeaders)
		if err != nil {
			common.WriteS3Error(c, common.AuthErr.WithMsg(err.Error()), string(c.Path()))
			c.Abort()
			return
		}

		scope := strings.Join([]string{info.Date, info.Region, info.Service, "aws4_request"}, "/")
		stringToSign := strings.Join([]string{
			s3Algorithm,
			amzDate,
			scope,
			sha256Hex(canonicalRequest),
		}, "\n")

		signingKey := s3SigningKey(sk, info.Date, info.Region, info.Service)
		expected := hex.EncodeToString(tools.HmacSHA256Bytes(signingKey, stringToSign))
		if !hmac.Equal([]byte(expected), []byte(info.Signature)) {
			common.WriteS3Error(c, common.AuthErr.WithMsg("invalid signature"), string(c.Path()))
			c.Abort()
			return
		}

		setUserContext(ctx, c, ak, sk)
		c.Next(ctx)
	}
}

func parseS3Authorization(header string) (*s3AuthInfo, error) {
	if !strings.HasPrefix(header, s3Algorithm+" ") {
		return nil, fmt.Errorf("missing s3 authorization")
	}

	values := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(header, s3Algorithm+" "), ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid authorization field")
		}
		values[kv[0]] = kv[1]
	}

	credential := strings.Split(values["Credential"], "/")
	if len(credential) != 5 || credential[4] != "aws4_request" {
		return nil, fmt.Errorf("invalid credential scope")
	}
	if values["SignedHeaders"] == "" || values["Signature"] == "" {
		return nil, fmt.Errorf("missing signature fields")
	}
	signedHeaders := strings.Split(values["SignedHeaders"], ";")
	for i, header := range signedHeaders {
		signedHeaders[i] = strings.ToLower(strings.TrimSpace(header))
	}
	sort.Strings(signedHeaders)

	return &s3AuthInfo{
		AccessKey:     credential[0],
		Date:          credential[1],
		Region:        credential[2],
		Service:       credential[3],
		SignedHeaders: signedHeaders,
		Signature:     values["Signature"],
	}, nil
}

func validS3RequestTime(amzDate string, window time.Duration) bool {
	t, err := time.Parse("20060102T150405Z", amzDate)
	if err != nil {
		return false
	}
	now := time.Now().UTC()
	return t.After(now.Add(-window)) && t.Before(now.Add(window))
}

func buildS3CanonicalRequest(c *app.RequestContext, signedHeaders []string) (string, error) {
	headers, err := canonicalS3Headers(c, signedHeaders)
	if err != nil {
		return "", err
	}
	payloadHash := strings.TrimSpace(string(c.GetHeader("x-amz-content-sha256")))
	if payloadHash == "" {
		payloadHash = "UNSIGNED-PAYLOAD"
	}

	return strings.Join([]string{
		string(c.Method()),
		s3URIEncodePath(string(c.Path())),
		canonicalS3Query(string(c.GetRequest().QueryString())),
		headers,
		strings.Join(signedHeaders, ";"),
		payloadHash,
	}, "\n"), nil
}

func canonicalS3Headers(c *app.RequestContext, signedHeaders []string) (string, error) {
	normalized := make([]string, 0, len(signedHeaders))
	for _, h := range signedHeaders {
		name := strings.ToLower(strings.TrimSpace(h))
		if name == "" {
			continue
		}
		normalized = append(normalized, name)
	}
	sort.Strings(normalized)

	lines := make([]string, 0, len(normalized))
	for _, name := range normalized {
		value := ""
		if name == "host" {
			value = string(c.Host())
		} else {
			value = string(c.GetHeader(name))
		}
		if value == "" {
			return "", fmt.Errorf("missing signed header %s", name)
		}
		lines = append(lines, name+":"+compressS3HeaderValue(value))
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func canonicalS3Query(rawQuery string) string {
	return canonicalQueryWithEscaper(rawQuery, s3URIEncode)
}

func s3URIEncodePath(path string) string {
	if path == "" {
		return "/"
	}
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = s3URIEncode(segment)
	}
	return strings.Join(segments, "/")
}

func s3URIEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func compressS3HeaderValue(v string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(v)), " ")
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func s3SigningKey(secret, date, region, service string) []byte {
	kDate := tools.HmacSHA256Bytes([]byte("AWS4"+secret), date)
	kRegion := tools.HmacSHA256Bytes(kDate, region)
	kService := tools.HmacSHA256Bytes(kRegion, service)
	return tools.HmacSHA256Bytes(kService, "aws4_request")
}
