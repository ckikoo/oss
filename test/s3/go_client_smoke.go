package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const service = "s3"

type client struct {
	endpoint  string
	accessKey string
	secretKey string
	region    string
	http      *http.Client
}

type createMultipartUploadResult struct {
	UploadID string `xml:"UploadId"`
}

func main() {
	var (
		endpoint = flag.String("endpoint", envDefault("S3_ENDPOINT", "http://127.0.0.1:8080"), "S3 endpoint")
		region   = flag.String("region", envDefault("AWS_REGION", "us-east-1"), "S3 signing region")
		bucket   = flag.String("bucket", envDefault("S3_BUCKET", ""), "bucket name")
		dryRun   = flag.Bool("dry-run", false, "compile/config check only")
	)
	flag.Parse()

	ak := os.Getenv("AWS_ACCESS_KEY_ID")
	sk := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if *dryRun {
		fmt.Println("[go s3 smoke] dry-run ok")
		return
	}
	if ak == "" || sk == "" {
		exitf("set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY")
	}
	if *bucket == "" {
		*bucket = fmt.Sprintf("s3-go-smoke-%d", time.Now().Unix())
	}

	c := &client{
		endpoint:  strings.TrimRight(*endpoint, "/"),
		accessKey: ak,
		secretKey: sk,
		region:    *region,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
	if err := run(context.Background(), c, *bucket); err != nil {
		exitf("%v", err)
	}
	fmt.Println("[go s3 smoke] ok")
}

func run(ctx context.Context, c *client, bucket string) error {
	fmt.Printf("[go s3 smoke] endpoint=%s bucket=%s\n", c.endpoint, bucket)
	defer cleanup(ctx, c, bucket)

	if _, err := c.do(ctx, http.MethodGet, "/", "", nil, nil); err != nil {
		return fmt.Errorf("list buckets: %w", err)
	}
	fmt.Println("[go s3 smoke] create/head/location bucket")
	if _, err := c.do(ctx, http.MethodPut, "/"+bucket, "", nil, nil); err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}
	if _, err := c.do(ctx, http.MethodHead, "/"+bucket, "", nil, nil); err != nil {
		return fmt.Errorf("head bucket: %w", err)
	}
	if _, err := c.do(ctx, http.MethodGet, "/"+bucket, "location=", nil, nil); err != nil {
		return fmt.Errorf("get bucket location: %w", err)
	}

	fmt.Println("[go s3 smoke] put/head/get object")
	hello := []byte("hello go sigv4 " + time.Now().UTC().Format(time.RFC3339) + "\n")
	if _, err := c.do(ctx, http.MethodPut, "/"+bucket+"/hello.txt", "", hello, map[string]string{"Content-Type": "text/plain"}); err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	if _, err := c.do(ctx, http.MethodHead, "/"+bucket+"/hello.txt", "", nil, nil); err != nil {
		return fmt.Errorf("head object: %w", err)
	}
	got, err := c.do(ctx, http.MethodGet, "/"+bucket+"/hello.txt", "", nil, nil)
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}
	if !bytes.Equal(got, hello) {
		return fmt.Errorf("download mismatch: got %q want %q", got, hello)
	}

	fmt.Println("[go s3 smoke] list/copy")
	if _, err := c.do(ctx, http.MethodGet, "/"+bucket, "list-type=2&prefix=hello&max-keys=10", nil, nil); err != nil {
		return fmt.Errorf("list objects v2: %w", err)
	}
	if _, err := c.do(ctx, http.MethodPut, "/"+bucket+"/copied.txt", "", nil, map[string]string{"x-amz-copy-source": "/" + bucket + "/hello.txt"}); err != nil {
		return fmt.Errorf("copy object: %w", err)
	}
	if _, err := c.do(ctx, http.MethodHead, "/"+bucket+"/copied.txt", "", nil, nil); err != nil {
		return fmt.Errorf("head copied object: %w", err)
	}

	fmt.Println("[go s3 smoke] multipart")
	uploadXML, err := c.do(ctx, http.MethodPost, "/"+bucket+"/multipart.bin", "uploads=", nil, map[string]string{"Content-Type": "application/octet-stream"})
	if err != nil {
		return fmt.Errorf("create multipart: %w", err)
	}
	var init createMultipartUploadResult
	if err := xml.Unmarshal(uploadXML, &init); err != nil {
		return fmt.Errorf("parse create multipart xml: %w", err)
	}
	if init.UploadID == "" {
		return fmt.Errorf("missing upload id")
	}

	etag1, err := c.uploadPart(ctx, bucket, init.UploadID, 1, []byte("part-one\n"))
	if err != nil {
		return err
	}
	etag2, err := c.uploadPart(ctx, bucket, init.UploadID, 2, []byte("part-two\n"))
	if err != nil {
		return err
	}
	if _, err := c.do(ctx, http.MethodGet, "/"+bucket+"/multipart.bin", "uploadId="+url.QueryEscape(init.UploadID), nil, nil); err != nil {
		return fmt.Errorf("list parts: %w", err)
	}
	completeXML := []byte(fmt.Sprintf(`<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part><Part><PartNumber>2</PartNumber><ETag>%s</ETag></Part></CompleteMultipartUpload>`, etag1, etag2))
	if _, err := c.do(ctx, http.MethodPost, "/"+bucket+"/multipart.bin", "uploadId="+url.QueryEscape(init.UploadID), completeXML, map[string]string{"Content-Type": "application/xml"}); err != nil {
		return fmt.Errorf("complete multipart: %w", err)
	}
	if _, err := c.do(ctx, http.MethodHead, "/"+bucket+"/multipart.bin", "", nil, nil); err != nil {
		return fmt.Errorf("head multipart object: %w", err)
	}

	fmt.Println("[go s3 smoke] delete objects and bucket")
	deleteXML := []byte(`<Delete><Object><Key>copied.txt</Key></Object><Object><Key>hello.txt</Key></Object><Object><Key>multipart.bin</Key></Object></Delete>`)
	if _, err := c.do(ctx, http.MethodPost, "/"+bucket, "delete=", deleteXML, map[string]string{"Content-Type": "application/xml"}); err != nil {
		return fmt.Errorf("delete objects: %w", err)
	}
	if _, err := c.do(ctx, http.MethodDelete, "/"+bucket, "", nil, nil); err != nil {
		return fmt.Errorf("delete bucket: %w", err)
	}
	return nil
}

func (c *client) uploadPart(ctx context.Context, bucket, uploadID string, partNumber int, body []byte) (string, error) {
	resp, err := c.doResp(ctx, http.MethodPut, "/"+bucket+"/multipart.bin", fmt.Sprintf("partNumber=%d&uploadId=%s", partNumber, url.QueryEscape(uploadID)), body, nil)
	if err != nil {
		return "", fmt.Errorf("upload part %d: %w", partNumber, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	etag := strings.TrimSpace(resp.Header.Get("ETag"))
	if etag == "" {
		return "", fmt.Errorf("upload part %d missing etag", partNumber)
	}
	return etag, nil
}

func cleanup(ctx context.Context, c *client, bucket string) {
	for _, key := range []string{"copied.txt", "hello.txt", "multipart.bin"} {
		c.do(ctx, http.MethodDelete, "/"+bucket+"/"+key, "", nil, nil)
	}
	c.do(ctx, http.MethodDelete, "/"+bucket, "", nil, nil)
}

func (c *client) do(ctx context.Context, method, path, rawQuery string, body []byte, headers map[string]string) ([]byte, error) {
	resp, err := c.doResp(ctx, method, path, rawQuery, body, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) doResp(ctx context.Context, method, path, rawQuery string, body []byte, headers map[string]string) (*http.Response, error) {
	target := c.endpoint + path
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c.sign(req, path, rawQuery, body)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s %s?%s failed: status=%d body=%s", method, path, rawQuery, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

func (c *client) sign(req *http.Request, path, rawQuery string, body []byte) {
	payloadHash := sha256Hex(body)
	amzDate := time.Now().UTC().Format("20060102T150405Z")
	dateScope := amzDate[:8]
	host := req.URL.Host

	req.Header.Set("Host", host)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", amzDate)

	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(path),
		canonicalQuery(rawQuery),
		"host:" + host + "\n" +
			"x-amz-content-sha256:" + payloadHash + "\n" +
			"x-amz-date:" + amzDate + "\n",
		signedHeaders,
		payloadHash,
	}, "\n")
	scope := dateScope + "/" + c.region + "/" + service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(signingKey(c.secretKey, dateScope, c.region, service), []byte(stringToSign)))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.accessKey+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = uriEncode(part)
	}
	return strings.Join(parts, "/")
}

func canonicalQuery(raw string) string {
	values, _ := url.ParseQuery(raw)
	type pair struct{ k, v string }
	pairs := make([]pair, 0)
	for k, vs := range values {
		for _, v := range vs {
			pairs = append(pairs, pair{k: k, v: v})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k == pairs[j].k {
			return pairs[i].v < pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, uriEncode(p.k)+"="+uriEncode(p.v))
	}
	return strings.Join(out, "&")
}

func uriEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func signingKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[go s3 smoke] "+format+"\n", args...)
	os.Exit(1)
}
