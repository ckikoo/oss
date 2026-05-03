# Multipart Upload Implementation Guide

## Overview

This document describes the multipart upload implementation in the OSS service, which uses a **virtual merge** strategy for optimal performance and storage efficiency.

## Architecture

### Components

- **multipart_uploads**: Stores upload session metadata
- **multipart_parts**: Stores individual part information
- **objects**: Final object record (logically merged)

### Virtual Merge Strategy

Unlike traditional multipart implementations that physically combine files, this implementation uses **virtual merging**:

1. **Upload Phase**: Parts are stored individually with metadata
2. **Complete Phase**: Creates object record without physical merge
3. **Read Phase**: Dynamically streams parts on-demand

#### Advantages

- **Performance**: No expensive merge operations
- **Storage Efficiency**: No duplicate storage during merge
- **Scalability**: Handles large files without memory constraints
- **Reliability**: Parts remain available if merge fails

#### Trade-offs

- **Read Performance**: Slightly slower first read due to streaming
- **Complexity**: Requires special handling in GetObject

## API Flow

### 1. Initialize Upload
```http
POST /api/v1/buckets/{bucket}/multipart/uploads
Content-Type: application/json

{
  "object_key": "large-file.zip",
  "content_type": "application/zip",
  "storage_class": "STANDARD"
}
```

**Response:**
```json
{
  "upload_id": "uuid-string",
  "bucket_id": 123,
  "object_key": "large-file.zip",
  "status": 0,
  "expires_at": 1640995200000
}
```

### 2. Upload Parts
```http
PUT /api/v1/buckets/{bucket}/multipart/uploads/{upload_id}/parts/{part_number}
X-Access-Key: AK
X-Secret-Key: SK
Content-Type: multipart/form-data

[file data]
```

**Response:**
```json
{
  "part_number": 1,
  "etag": "md5-hash",
  "size": 5242880,
  "status": 1
}
```

### 3. Complete Upload
```http
POST /api/v1/buckets/{bucket}/multipart/uploads/{upload_id}/complete
Content-Type: application/json

{
  "parts": [
    {"part_number": 1, "etag": "md5-hash-1"},
    {"part_number": 2, "etag": "md5-hash-2"}
  ]
}
```

**Response:**
```json
{
  "object_id": 456,
  "object_key": "large-file.zip",
  "version_id": "",
  "status": 1
}
```

## Database Schema

### multipart_uploads
```sql
CREATE TABLE multipart_uploads (
  upload_id VARCHAR(36) PRIMARY KEY,
  bucket_id BIGINT,
  bucket_name VARCHAR(255),
  object_key VARCHAR(1024),
  object_key_hash VARCHAR(32),
  user_id BIGINT,
  total_chunk INT DEFAULT 0,
  uploaded_chunk INT DEFAULT 0,
  status TINYINT DEFAULT 0,
  storage_class VARCHAR(20),
  content_type VARCHAR(255),
  metadata JSON,
  expires_at DATETIME,
  last_active_at DATETIME,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### multipart_parts
```sql
CREATE TABLE multipart_parts (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  upload_id VARCHAR(36),
  part_number INT,
  size BIGINT,
  etag VARCHAR(32),
  storage_path VARCHAR(1024),
  status TINYINT DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_upload_part (upload_id, part_number)
);
```

## Implementation Details

### ETag Calculation

For multipart objects, ETag is calculated as:
```
MD5(part1-etag + part2-etag + ... + partN-etag + "-" + N)
```

### Storage Paths

- **Parts**: `/storage/{bucket}/multipart/{upload_id}/part_{number}`
- **Regular Objects**: `/storage/{bucket}/{object_key}`

### Status Constants

- **Upload Status**: 0=Uploading, 1=MergedVirtual, 2=MergedPhysical, 3=Failed, 4=Aborted
- **Part Status**: 0=Uploading, 1=Confirmed, 2=Merged

### Cleanup Strategy

- **Abort**: Deletes all parts and upload record
- **Expiration**: Redis-based timeout monitoring
- **Delete Object**: Soft delete with physical cleanup

## Performance Considerations

### Memory Usage
- Parts are streamed directly to disk
- No in-memory buffering for large files
- GetObject uses chunked transfer encoding

### Concurrent Access
- Multiple parts can be uploaded simultaneously
- Complete operation is atomic
- Read operations are thread-safe

### Monitoring
- Upload progress tracking via `uploaded_chunk`
- Timeout handling via Redis
- Error logging for failed operations

## Future Enhancements

1. **Physical Merge**: Optional background merge for frequently accessed files
2. **Compression**: Part-level compression
3. **Deduplication**: Content-based deduplication across parts
4. **CDN Integration**: Direct part serving from CDN
5. **Progress Callbacks**: Real-time upload progress notifications</content>
<parameter name="filePath">e:\Desktop\新建文件夹\oss\MULTIPART_GUIDE.md