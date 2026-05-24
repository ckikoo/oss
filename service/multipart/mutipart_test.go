package multipart

import "testing"

func TestCompleteMultipartTotalChunkKeepsExistingValue(t *testing.T) {
	if got := completeMultipartTotalChunk(5, 2); got != 5 {
		t.Fatalf("completeMultipartTotalChunk = %d, want 5", got)
	}
}

func TestCompleteMultipartTotalChunkUsesPartsCountForS3Mode(t *testing.T) {
	if got := completeMultipartTotalChunk(0, 2); got != 2 {
		t.Fatalf("completeMultipartTotalChunk = %d, want 2", got)
	}
}
