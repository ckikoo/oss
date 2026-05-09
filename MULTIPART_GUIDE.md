# Multipart Upload Implementation Guide

## Overview

This document describes the multipart upload implementation in the OSS service, which uses a **virtual merge** strategy for optimal performance and storage efficiency.

## Architecture

### Components

- **multipart_uploads**: Stores upload session metadata
- **multipart_parts**: Stores individual part information
- **objects**: Final object record (logically merged)

### Virtual Merge Strategy

Unlike traditional multipart implementations that physically combine files during complete, this implementation uses **virtual merging** for the API path while deferring physical merge to a background task:

1. **Upload Phase**: Parts are stored individually with metadata
2. **Complete Phase**: Creates object record and marks the upload as virtually merged
3. **Background Merge**: Publishes a `PHYSICAL_MERGE` async task for offline physical merge
4. **Read Phase**: Dynamically streams parts on-demand until physical merge completes

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

**实现说明**:
- `CompleteMultipartUpload` 验证所有上传分片的 `etag` 是否一致
- 创建最终 `objects` 记录，并将 multipart 会话标记为虚拟合并（`MergedVirtual`）
- 发布异步任务 `PHYSICAL_MERGE` 到 Redis 队列，任务由 `timer/timer.go` 后台 worker 消费
- 后台 worker 调用 `storage.MergeParts(ctx, bucket, objectKey, partPaths)` 完成物理合并，并在成功后更新对象元数据
- 如果物理合并失败，异步任务会更新状态为失败，等待重试或人工补偿

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

- **Abort**: 触发 `ABORT_MULTIPART` 异步任务，后台 worker 清理分片和 multipart 会话
- **Expiration**: 使用 Redis 超时监控，定期扫描并取消超时 multipart 上传
- **Delete Object**: 对象删除操作与物理文件清理解耦，删除时可以优先软删除元数据并异步清理存储

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

1. **Physical Merge Optimization**: Improve background merge worker throughput and retry behavior for hot multipart objects
2. **Compression**: Part-level compression
3. **Deduplication**: Content-based deduplication across parts
4. **CDN Integration**: Direct part serving from CDN
5. **Progress Callbacks**: Real-time upload progress notifications</content>
<parameter name="filePath">e:\Desktop\新建文件夹\oss\MULTIPART_GUIDE.md