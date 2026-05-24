package timer

import "testing"

func TestMultipartPartsCountMismatchSkipsUnknownTotalChunk(t *testing.T) {
	if multipartPartsCountMismatch(2, 0) {
		t.Fatal("total_chunk=0 should allow S3 multipart with unknown initial part count")
	}
}

func TestMultipartPartsCountMismatchChecksKnownTotalChunk(t *testing.T) {
	if multipartPartsCountMismatch(2, 2) {
		t.Fatal("matching known total_chunk should be accepted")
	}
	if !multipartPartsCountMismatch(2, 3) {
		t.Fatal("mismatching known total_chunk should be rejected")
	}
}
