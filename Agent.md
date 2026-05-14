# OSS 数据库设计 Agent

## 目标
该文档用于让 AI 理解当前 OSS 项目的数据库设计、核心表关系和认证模型。它帮助分析 `init.sql`、配合代码结构理解认证、权限和对象存储流程。

## 设计原则
- `users` 表保存账户基础信息，主要用于管理和统计。
- `access_keys` 表负责 AK/SK 认证，是对外 API 密钥入口。
- `buckets`、`objects`、`multipart_uploads` 等表是 OSS 核心资源。
- 复杂权限使用策略表结构，允许从简单 ACL 扩展到细粒度控制。
- 目前数据库设计侧重可扩展性与业务分层，实现 MVP 到后续扩展。
- 常量全部放在 `consts` 包里，禁止直接写魔法字符串或数字。

## 认证模型
- API 请求通过 AK/SK 认证，AK 在 `access_keys` 表中查找，SK 通过 SHA256 哈希验证。
- 支持两种认证方式：
  - HTTP Header: `X-Access-Key` 和 `X-Secret-Key`
  - Authorization Header: `AccessKey AK:SK`
- 认证中间件 `NewAccessKeyMiddleware` 保护 bucket、object 和 multipart API。
- 权限通过 `access_keys.permission` JSON 字段控制，支持细粒度权限。

## Multipart Upload 流程
- `CreateMultipartUpload`: 初始化会话，生成 `upload_id`，创建 `multipart_uploads` 记录。
- `UploadMultipartPart`: 上传分片，保存到 `multipart_parts`，更新会话进度。
- `CompleteMultipartUpload`: 虚拟合并，创建最终 `objects` 记录，标记会话完成。不进行物理文件合并。
- `AbortMultipartUpload`: 中止会话，清理分片和会话记录。
- **虚拟合并策略**: 不实际合并文件，在读取时动态流式输出分片内容。

### 优势
- 避免大文件合并的IO开销
- 支持超大文件上传
- 读取时流式处理，避免OOM
- 分片独立存储，便于管理

## 核心表说明

### users
- `id`: 用户主键
- `email`: 唯一邮箱
- `status`: 用户状态，1=正常，2=禁用，3=注销
- `storage_quota`: 存储配额
- `storage_used`: 已用存储

用途：用户账号信息，不直接作为 API 认证凭证。

### access_keys
- `id`: 主键
- `user_id`: 关联用户 ID
- `access_key`: AK 公开标识
- `secret_key`: SK，加密保存
- `alias`: 别名/备注
- `status`: 1=启用，0=禁用
- `permission`: JSON 格式细粒度权限
- `last_used_at`: 最近使用时间
- `expires_at`: 过期时间

用途：AK/SK 认证入口，API 请求通过该表验证权限。

### buckets
- `id`: 主键
- `user_id`: 所属用户
- `name`: 全局唯一 Bucket 名称
- `region`: 区域
- `acl`: 基础访问控制
- `versioning`: 是否启用版本控制
- `status`: 状态
- `storage_class`: 存储类型，默认值 `STANDARD`，在代码中统一定义为 `consts.StorageClassStandard`，可选 `STANDARD`/`IA`/`ARCHIVE`
- `object_count`, `storage_size`: 统计字段

用途：表示 OSS 空间属性和访问控制。

### objects
- `bucket_id`, `bucket_name`: 所属 Bucket
- `object_key`, `object_key_hash`: 对象路径及其 MD5 哈希，用于唯一索引
- `version_id`: 版本 ID
- `size`, `etag`, `content_type`, `storage_class`
- `is_multipart`, `upload_id`, `storage_path`
- `acl`: 对象级 ACL
- `metadata`: JSON 元数据
- `status`: 1=正常，2=删除标记，3=物理删除

用途：保存对象元数据和存储路径信息。

存储类型：对象层 `storage_class` 默认继承 `STANDARD`，该默认值由 `consts.StorageClassStandard` 定义。

### multipart_uploads
- `upload_id`: 分片会话 ID
- `bucket_id`, `bucket_name`
- `object_key`, `object_key_hash`
- `user_id`
- `total_chunk`, `uploaded_chunk`
- `status`: 上传状态
- `expires_at`, `last_active_at`

用途：分片上传会话管理。

### multipart_parts
- `upload_id`, `part_number`, `size`, `etag`, `storage_path`
- `status`: 分片状态

## 分布式锁机制

项目实现了基于 Redis 的分布式文件锁机制，用于控制对同一文件对象的并发访问。

### 锁设计原则

- **基于 Bucket + Object**: 锁的粒度为 `{bucketName}:{objectName}`，确保同一对象的操作是串行的
- **UUID 标识**: 每个锁由唯一的 UUID 标识，确保安全性
- **自动过期**: 所有锁都有 TTL，避免死锁
- **原子操作**: 使用 Redis Lua 脚本确保操作的原子性
- **Key 格式**: `{ServerName}:lock:file:{bucketName}:{objectName}`

### 锁操作流程

1. **获取锁**: 使用 `SET NX` 命令原子性设置锁，如果成功则获得锁的控制权
2. **持有锁**: 在锁的有效期内执行文件操作
3. **续期锁**: 可主动延长锁的过期时间，避免操作过程中锁过期
4. **释放锁**: 使用 Lua 脚本验证锁的持有者身份后释放锁
5. **强制释放**: 管理员可强制释放锁，用于清理异常情况

### 锁接口设计

```go
type IFileLock interface {
    AcquireLock(ctx context.Context, bucketName string, objectName string, uuid string, ttl time.Duration) (bool, error)
    ReleaseLock(ctx context.Context, bucketName string, objectName string, uuid string) error
    RefreshLock(ctx context.Context, bucketName string, objectName string, uuid string, ttl time.Duration) (bool, error)
    CheckLock(ctx context.Context, bucketName string, objectName string, uuid string) (bool, error)
    ForceReleaseLock(ctx context.Context, bucketName string, objectName string) error
}
```

### 安全考虑

- **防止误释放**: 释放锁时必须验证 UUID，只有锁的持有者才能释放
- **防止死锁**: 所有锁都有过期时间，即使客户端崩溃也不会永久占用锁
- **并发安全**: 使用 Redis 的原子操作确保多客户端并发访问的安全性
- **性能优化**: 基于内存的 Redis 操作，锁操作延迟极低

### 使用场景

- **文件上传**: 防止同一文件被多个客户端同时上传
- **文件删除**: 确保删除操作的原子性
- **元数据更新**: 保护并发修改对象元数据的安全性
- **分片上传**: 控制 multipart upload 会话的并发访问

用途：保存每个分片详细信息。

### async_tasks
- `task_id`, `task_type`, `upload_id`, `object_id`
- `status`, `progress`, `result`, `error_msg`, `retry_count`

用途：异步处理任务，如物理合并、转码等。

### bucket_policies 与策略表
- `bucket_policies`: 策略头表
- `policy_principals`: 策略主体
- `policy_actions`: 策略操作
- `policy_resources`: 策略资源范围
- `policy_conditions`: 附加条件

用途：实现细粒度权限策略，适合复杂访问控制场景。

当前实现中，`ListBucketPolicies` 会并发加载每条策略的主体、操作、资源和条件子表，使用 `oss/utils/pool` 控制并发数量，避免在策略数量多时出现大量串行查询。

### lifecycle_rules
- `rule_name`, `prefix`, `transition_days`, `expiration_days`

用途：对象生命周期管理。

### metering_daily
- `user_id`, `bucket_id`, `stat_date`
- `storage_size`: 存储量变更（上传增加、删除减少）
- `object_count`: 对象数量变更
- `upload_flow`: PUT/上传流量
- `download_flow`: GET/下载流量，基于实际传输字节统计
- `get_request_count`: GET 请求次数
- `put_request_count`: PUT 请求次数
- `del_request_count`: DELETE 请求次数

用途：计费与统计。当前实现支持 bucket 级明细统计和 `bucket_id=NULL` 的用户总计统计，查询接口为 `GET /api/v1/metrics/daily`，可按 `user_id`、`bucket_id`、`date_from`、`date_to` 过滤。

实现说明：对象下载使用流式输出并通过 `io.MultiWriter` 统计真实下行字节，避免依赖对象元数据 `Size`。
### operation_logs
- 记录请求、状态、耗时、IP 等审计信息

用途：审计和性能分析。

### event_rules / event_deliveries
- 事件规则与投递记录

用途：Webhook/MQ/FUNC 事件通知扩展。

## AI 使用说明
- 认证应优先基于 `access_keys` 的 AK/SK。
- `users` 仅作为账号信息与业务归属。
- 简单权限可使用 `buckets.acl`，复杂权限使用策略表。
- 查询对象时可利用 `object_key_hash` 避免长索引问题。
- 分片上传逻辑由 `multipart_uploads` 和 `multipart_parts` 管理。
- 预签名 URL 与事件通知机制为可扩展模块。

## 备注
- 当前 SQL 设计不强制外键约束，关联一致性由业务层保障。
- 该设计适用于从基础 OSS MVP 到后续扩展的服务架构。
