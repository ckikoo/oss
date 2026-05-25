# OSS API 快速参考

## 认证

### Authorization 签名
```
Authorization: OSS <access_key>:<timestamp>:<signature>
```

### 临时 Token
```
GET /api/v1/buckets/{bucket}/objects/{key}?token={token}
```

---

## 缓存策略（简要）

- 对于稳定的只读元数据（例如 `bucket`、`object` 元数据、`video` 的 transcode/profile 信息），系统采用本地 LRU + Redis 的分层缓存以降低读延迟。写操作会触发失效并通过发布/订阅广播到其他实例，详细设计见 `doc/VIDEO_CACHE_DESIGN.md`。
- 对于高写且易变的数据（例如 multipart uploads / multipart parts），不使用跨实例缓存，直接以数据库为单一可信数据源；相关考量见 `doc/MULTIPART_GUIDE.md` 的“缓存考虑”节。


## Bucket 操作

### 创建 Bucket
```bash
POST /api/v1/buckets
Authorization: OSS <ak>:<ts>:<sig>

{
  "name": "my-bucket",
  "region": "cn-hz",
  "acl": "private",
  "versioning": true,
  "storage_class": "STANDARD"
}
```

### 列表 Bucket
```bash
GET /api/v1/buckets
Authorization: OSS <ak>:<ts>:<sig>

?limit=10&offset=0
```

### 获取 Bucket 信息
```bash
GET /api/v1/buckets/{bucket}
Authorization: OSS <ak>:<ts>:<sig>
```

### 更新 Bucket
```bash
PUT /api/v1/buckets/{bucket}
Authorization: OSS <ak>:<ts>:<sig>

{
  "acl": "public-read",
  "versioning": true
}
```

### 删除 Bucket
```bash
DELETE /api/v1/buckets/{bucket}
Authorization: OSS <ak>:<ts>:<sig>
```

---

## Object 操作

### 上传对象
```bash
PUT /api/v1/buckets/{bucket}/objects/{key}
Authorization: OSS <ak>:<ts>:<sig>
Content-Type: multipart/form-data

file=@/path/to/file
storage_class=STANDARD
acl=default
metadata={"key":"value"}
```

### 下载对象
```bash
GET /api/v1/buckets/{bucket}/objects/{key}
Authorization: OSS <ak>:<ts>:<sig>

# 使用 Token
GET /api/v1/buckets/{bucket}/objects/{key}?token={token}
```

### 获取对象元数据
```bash
GET /api/v1/buckets/{bucket}/objects/{key}/metadata
Authorization: OSS <ak>:<ts>:<sig>

?version_id=<version_id>
```

### 列表对象
```bash
GET /api/v1/buckets/{bucket}/objects
Authorization: OSS <ak>:<ts>:<sig>

?prefix=log/
?delimiter=/
?max_keys=100
?marker=<next_marker>
?storage_class=STANDARD
?content_type=application/json
?created_at_start=1716508800000
?created_at_end=1716595200000
```

响应包含 `items`、`common_prefixes`、`next_marker`、`is_truncated`、`max_keys`。
当 `is_truncated=true` 时，下一页继续传 `marker=<next_marker>`；`next_marker` 是分页游标，不保证等于对象 key。

### 删除对象
```bash
DELETE /api/v1/buckets/{bucket}/objects/{key}
Authorization: OSS <ak>:<ts>:<sig>

?version_id=<version_id>  # 删除指定版本
```

### 获取对象版本历史
```bash
GET /api/v1/buckets/{bucket}/objects/{key}/versions
Authorization: OSS <ak>:<ts>:<sig>
```

---

## 分片上传

### 初始化
```bash
POST /api/v1/buckets/{bucket}/multipart/uploads
Authorization: OSS <ak>:<ts>:<sig>

{
  "object_key": "large-file.zip",
  "storage_class": "STANDARD",
  "content_type": "application/zip"
}

# 返回
{
  "upload_id": "uuid",
  "expires_at": 1640995200000
}
```

### 上传分片
```bash
PUT /api/v1/buckets/{bucket}/multipart/uploads/{upload_id}/parts/{part_number}
Authorization: OSS <ak>:<ts>:<sig>
Content-Type: multipart/form-data

file=@/path/to/part

# 返回
{
  "part_number": 1,
  "etag": "md5-hash",
  "size": 5242880
}
```

### 完成上传
```bash
POST /api/v1/buckets/{bucket}/multipart/uploads/{upload_id}/complete
Authorization: OSS <ak>:<ts>:<sig>

{
  "parts": [
    {"part_number": 1, "etag": "hash1"},
    {"part_number": 2, "etag": "hash2"}
  ]
}

# 返回
{
  "object_id": 456,
  "version_id": "v1"
}
```

### 中止上传
```bash
DELETE /api/v1/buckets/{bucket}/multipart/uploads/{upload_id}
Authorization: OSS <ak>:<ts>:<sig>
```

---

## 权限策略

### 创建 Policy
```bash
POST /api/v1/buckets/{bucket}/policies
Authorization: OSS <ak>:<ts>:<sig>

{
  "name": "allow-read",
  "effect": "Allow",
  "principals": [
    {"type": "User", "value": "user:123"}
  ],
  "actions": ["GetObject", "ListObjects"],
  "resources": ["arn:oss:::{bucket}/*"],
  "conditions": []
}
```

### 列表 Policy
```bash
GET /api/v1/buckets/{bucket}/policies
Authorization: OSS <ak>:<ts>:<sig>

?limit=10&offset=0
```

---

## 生命周期规则

### 创建规则
```bash
POST /api/v1/buckets/{bucket}/lifecycle
Authorization: OSS <ak>:<ts>:<sig>

{
  "rule_name": "transition-rule",
  "status": 1,
  "prefix": "logs/",
  "transition_days": 30,
  "transition_storage_class": "IA",
  "expiration_days": 180
}
```

### 列表规则
```bash
GET /api/v1/buckets/{bucket}/lifecycle
Authorization: OSS <ak>:<ts>:<sig>
```

### 获取规则
```bash
GET /api/v1/buckets/{bucket}/lifecycle/{rule_id}
Authorization: OSS <ak>:<ts>:<sig>
```

### 更新规则
```bash
PUT /api/v1/buckets/{bucket}/lifecycle/{rule_id}
Authorization: OSS <ak>:<ts>:<sig>

{
  "rule_name": "updated-rule",
  "transition_days": 60
}
```

### 删除规则
```bash
DELETE /api/v1/buckets/{bucket}/lifecycle/{rule_id}
Authorization: OSS <ak>:<ts>:<sig>
```

---

## 事件规则

### 创建规则
```bash
POST /api/v1/buckets/{bucket}/event-rules
Authorization: OSS <ak>:<ts>:<sig>

{
  "rule_name": "backup-rule",
  "events": ["s3:ObjectCreated:*"],
  "filter": {
    "prefix": "backup/",
    "suffix": ".csv"
  },
  "destination": "http://webhook.example.com/notify"
}
```

### 列表规则
```bash
GET /api/v1/buckets/{bucket}/event-rules
Authorization: OSS <ak>:<ts>:<sig>
```

### 更新规则
```bash
PUT /api/v1/buckets/{bucket}/event-rules/{rule_id}
Authorization: OSS <ak>:<ts>:<sig>

{
  "rule_name": "updated-rule",
  "events": ["s3:ObjectRemoved:*"]
}
```

### 删除规则
```bash
DELETE /api/v1/buckets/{bucket}/event-rules/{rule_id}
Authorization: OSS <ak>:<ts>:<sig>
```

---

## CORS 规则

### 创建 CORS 规则
```bash
POST /api/v1/buckets/{bucket}/cors
Authorization: OSS <ak>:<ts>:<sig>

{
  "allowed_origins": ["https://example.com"],
  "allowed_methods": ["GET", "PUT", "POST"],
  "allowed_headers": ["Content-Type"],
  "expose_headers": ["ETag"],
  "max_age_seconds": 3600
}
```

### 列表 CORS 规则
```bash
GET /api/v1/buckets/{bucket}/cors
Authorization: OSS <ak>:<ts>:<sig>
```

### 删除 CORS 规则
```bash
DELETE /api/v1/buckets/{bucket}/cors/{rule_id}
Authorization: OSS <ak>:<ts>:<sig>
```

---

## Token 管理

### 生成上传 Token
```bash
POST /api/v1/upload/tokens
Authorization: OSS <ak>:<ts>:<sig>

{
  "bucket": "my-bucket",
  "key": "uploads/file.txt",
  "ttl_seconds": 3600
}

# 返回
{
  "token": "eyJ...",
  "expires_at": 1640995200000
}
```

### 生成下载 Token
```bash
POST /api/v1/download/tokens
Authorization: OSS <ak>:<ts>:<sig>

{
  "bucket": "my-bucket",
  "key": "files/file.txt",
  "ttl_seconds": 3600
}
```

---

## 统计指标

### 查询日级统计
```bash
GET /api/v1/metrics/daily
Authorization: OSS <ak>:<ts>:<sig>

?user_id=123
?bucket_id=456
?date_from=2026-05-01
?date_to=2026-05-31
```

### 返回字段
```json
{
  "date": "2026-05-22",
  "storage_size": 1073741824,
  "object_count": 100,
  "upload_flow": 536870912,
  "download_flow": 268435456,
  "get_request_count": 1000,
  "put_request_count": 500,
  "del_request_count": 100
}
```

---

## 审计日志

### 查询操作日志
```bash
GET /api/v1/audit/logs
Authorization: OSS <ak>:<ts>:<sig>

?user_id=123
?bucket_id=456
?operation_type=PutObject
?date_from=2026-05-01
?date_to=2026-05-31
?limit=100
?offset=0
```

---

## 视频处理

### 生成播放 Token
```bash
POST /api/v1/video/play-tokens
Authorization: OSS <ak>:<ts>:<sig>

{
  "bucket": "my-bucket",
  "object_key": "videos/movie.mp4",
  "ttl_seconds": 3600
}

# 返回
{
  "token": "eyJ...",
  "play_url": "https://oss.example.com/api/v1/video/play?token=...",
  "expires_at": 1640995200000
}
```

### 获取转码状态
```bash
GET /api/v1/video/transcodes
Authorization: OSS <ak>:<ts>:<sig>

?bucket={bucket}
?object_key={key}
```

---

## Access Key 管理

### 创建 Access Key
```bash
POST /api/v1/access-keys
Authorization: OSS <ak>:<ts>:<sig>

{
  "description": "My API Key"
}

# 返回
{
  "access_key": "AKIA...",
  "secret_key": "wJal...",
  "status": 1
}
```

### 列表 Access Key
```bash
GET /api/v1/access-keys
Authorization: OSS <ak>:<ts>:<sig>

?limit=10&offset=0
```

### 更新 Access Key 状态
```bash
PUT /api/v1/access-keys/{access_key}
Authorization: OSS <ak>:<ts>:<sig>

{
  "status": 2  # 1=enabled, 2=disabled
}
```

---

## 常用命令

### 生成代码
```bash
# GORM Model 和 Query
go run ./tools/gen.go

# 对象转换器
goverter gen ./service
```

### 启动服务
```bash
go run ./cmd/server/main.go

# 使用自定义配置
go run ./cmd/server/main.go -c /path/to/config.yaml
```

### 初始化数据库
```bash
mysql -uroot -p < init.sql
```

### 生成 Access Key
```bash
curl -X POST http://localhost:8080/api/v1/access-keys \
  -H "Authorization: OSS <ak>:<ts>:<sig>" \
  -H "Content-Type: application/json" \
  -d '{"description": "test key"}'
```

---

## 存储类型常量

- `STANDARD` - 标准存储（热数据）
- `IA` - 低频访问（温数据，30天以上）
- `ARCHIVE` - 归档存储（冷数据，90天以上）

---

## 常见错误码

| 错误 | 含义 | 处理方式 |
|------|------|--------|
| 401 | 认证失败 | 检查 AK/SK 和签名 |
| 403 | 权限不足 | 检查 Bucket Policy 和 Token 权限 |
| 404 | 资源不存在 | 检查 Bucket/Object 是否存在 |
| 409 | 冲突 | Bucket 已存在或分片数据冲突 |
| 500 | 服务器错误 | 检查日志 |

---

## 相关文档

- [完整项目索引](PROJECT_INDEX.md)
- [分片上传详解](MULTIPART_GUIDE.md)
- [权限 API 文档](POLICY_API.md)
- [对象版本设计](OBJECT_VERSIONING_DESIGN.md)
- [视频处理计划](video.md)
- [异步任务流程](task.md)
