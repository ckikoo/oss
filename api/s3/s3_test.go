package s3

import (
	"io"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
)

func TestRequestBodyReaderUsesBodyStream(t *testing.T) {
	var c app.RequestContext
	c.Request.SetBodyStream(strings.NewReader("stream-body"), len("stream-body"))

	reader, err := requestBodyReader(&c)
	if err != nil {
		t.Fatalf("requestBodyReader returned error: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read body stream: %v", err)
	}
	if string(got) != "stream-body" {
		t.Fatalf("body = %q, want stream-body", got)
	}
}

func TestRequestBodyReaderFallsBackToBufferedBody(t *testing.T) {
	var c app.RequestContext
	c.Request.SetBodyString("buffered-body")

	reader, err := requestBodyReader(&c)
	if err != nil {
		t.Fatalf("requestBodyReader returned error: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read buffered body: %v", err)
	}
	if string(got) != "buffered-body" {
		t.Fatalf("body = %q, want buffered-body", got)
	}
}

func TestParseCopySourceDecodesPathAndDropsQuery(t *testing.T) {
	bucket, key := parseCopySource("/source-bucket/path/a%20b.txt?versionId=v1")

	if bucket != "source-bucket" {
		t.Fatalf("bucket = %q, want source-bucket", bucket)
	}
	if key != "path/a b.txt" {
		t.Fatalf("key = %q, want path/a b.txt", key)
	}
}

func TestParseCopySourceRejectsMissingKeyByReturningEmptyKey(t *testing.T) {
	bucket, key := parseCopySource("/source-bucket")

	if bucket != "source-bucket" || key != "" {
		t.Fatalf("bucket/key = %q/%q, want source-bucket/<empty>", bucket, key)
	}
}

func TestParseCompleteMultipartUploadXML(t *testing.T) {
	body := []byte(`<CompleteMultipartUpload>
		<Part><PartNumber>1</PartNumber><ETag>"etag-1"</ETag></Part>
		<Part><PartNumber>2</PartNumber><ETag>etag-2</ETag></Part>
	</CompleteMultipartUpload>`)

	parts, err := parseCompleteMultipartUploadXML(body)
	if err != nil {
		t.Fatalf("parseCompleteMultipartUploadXML returned error: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("len(parts) = %d, want 2", len(parts))
	}
	if parts[0].PartNumber != 1 || parts[0].ETag != `"etag-1"` {
		t.Fatalf("part[0] = %+v, want part 1 with quoted etag", parts[0])
	}
	if parts[1].PartNumber != 2 || parts[1].ETag != "etag-2" {
		t.Fatalf("part[1] = %+v, want part 2", parts[1])
	}
}

func TestParseCompleteMultipartUploadXMLRejectsEmptyParts(t *testing.T) {
	_, err := parseCompleteMultipartUploadXML([]byte(`<CompleteMultipartUpload></CompleteMultipartUpload>`))
	if err == nil {
		t.Fatal("expected empty parts error")
	}
}

func TestParseCompleteMultipartUploadXMLRejectsInvalidPart(t *testing.T) {
	_, err := parseCompleteMultipartUploadXML([]byte(`<CompleteMultipartUpload><Part><PartNumber>0</PartNumber><ETag></ETag></Part></CompleteMultipartUpload>`))
	if err == nil {
		t.Fatal("expected invalid part error")
	}
}

func TestParseDeleteObjectsXML(t *testing.T) {
	body := []byte(`<Delete><Quiet>true</Quiet><Object><Key>a.txt</Key></Object><Object><Key>dir/b.txt</Key><VersionId>v1</VersionId></Object></Delete>`)

	req, err := parseDeleteObjectsXML(body)
	if err != nil {
		t.Fatalf("parseDeleteObjectsXML returned error: %v", err)
	}
	if !req.Quiet {
		t.Fatal("Quiet = false, want true")
	}
	if len(req.Objects) != 2 {
		t.Fatalf("len(objects) = %d, want 2", len(req.Objects))
	}
	if req.Objects[1].Key != "dir/b.txt" || req.Objects[1].VersionID != "v1" {
		t.Fatalf("object[1] = %+v, want key dir/b.txt version v1", req.Objects[1])
	}
}

func TestParseDeleteObjectsXMLRejectsEmptyObjects(t *testing.T) {
	_, err := parseDeleteObjectsXML([]byte(`<Delete></Delete>`))
	if err == nil {
		t.Fatal("expected empty objects error")
	}
}

func TestParseDeleteObjectsXMLRejectsBlankKey(t *testing.T) {
	_, err := parseDeleteObjectsXML([]byte(`<Delete><Object><Key> </Key></Object></Delete>`))
	if err == nil {
		t.Fatal("expected blank key error")
	}
}
