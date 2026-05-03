package do

import "time"

type MultipartUploadDo struct {
	ID            int64
	UploadID      string
	BucketID      int64
	BucketName    string
	ObjectKey     string
	ObjectKeyHash string
	UserID        int64
	TotalChunk    int32
	UploadedChunk int32
	Status        int32
	StorageClass  *string
	ContentType   *string
	Metadata      *string
	ExpiresAt     time.Time
	LastActiveAt  time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateMultipartUpload struct {
	UploadID      string
	BucketID      int64
	BucketName    string
	ObjectKey     string
	ObjectKeyHash string
	UserID        int64
	TotalChunk    int32
	UploadedChunk int32
	Status        int32
	StorageClass  *string
	ContentType   *string
	Metadata      *string
	ExpiresAt     time.Time
	LastActiveAt  time.Time
}

type UpdateMultipartUpload struct {
	TotalChunk   *int32
	Status       *int32
	StorageClass *string
	ContentType  *string
	Metadata     *string
	ExpiresAt    *time.Time
	LastActiveAt *time.Time
}

type MultipartPartDo struct {
	ID          int64
	UploadID    string
	PartNumber  int32
	Size        int64
	Etag        string
	StoragePath string
	Status      int32
	CreatedAt   time.Time
}

type CreateMultipartPart struct {
	UploadID    string
	PartNumber  int32
	Size        int64
	Etag        string
	StoragePath string
	Status      int32
}

type UpdateMultipartPart struct {
	Size        *int64
	Etag        *string
	StoragePath *string
	Status      *int32
}
