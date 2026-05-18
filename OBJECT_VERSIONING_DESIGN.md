# Object Versioning, Delete, and Rollback Design

本文档定义对象版本控制、删除标记、永久删除和版本回滚的目标语义。它用于后续实现、接口评审、测试用例和生命周期任务对齐。

## 设计目标

- 对外语义稳定：同一个对象 key 的 PUT、GET、DELETE、ROLLBACK 行为可预测。
- 历史可审计：回滚不篡改历史版本，而是产生新的最新版本。
- 元数据强一致：`objects` 表中的 `is_latest`、`status` 和统计字段在事务内一次性更新。
- 存储 I/O 解耦：Storage 读写、复制、删除不放入数据库事务。
- 并发可控：同一个 bucket/object key 的并发写入、删除、回滚不能产生多个 latest。

## 核心术语

| 术语 | 含义 |
|---|---|
| object key | 用户看到的对象路径，例如 `dir/a.txt` |
| version_id | 对象版本 ID，建议所有写入都生成内部版本 ID |
| latest | 同一个 bucket/object key 当前可见的最新版本，`is_latest=1` |
| normal version | 有真实对象内容的版本，`status=1` |
| delete marker | 删除标记版本，`status=2`，无真实文件内容 |
| purged version | 已永久删除版本，`status=3` |
| rollback/restore | 从指定历史版本恢复，恢复结果是一个新版本 |

## 状态定义

`objects.status`：

```text
1 = normal
2 = delete_marker
3 = purged
```

`objects.is_latest`：

```text
1 = 当前 key 的最新版本，包括 delete_marker
0 = 历史版本
```

一个 `(bucket_id, object_key_hash)` 在 `status<>3` 的记录里最多只能有一条 `is_latest=1`。

## Versioning 模式

建议统一 `buckets.versioning` 常量和 SQL 注释：

```text
1 = disabled
2 = enabled
```

当前 `consts` 已按 `1/2` 定义，`init.sql` 注释仍是 `0/1`，落地前需要修正，避免服务层判断和数据库默认值不一致。

### disabled

`versioning=disabled` 表示不对外承诺历史版本能力：

| 操作 | 语义 |
|---|---|
| PUT | 创建新的内部版本，旧的 latest 被标记为 `purged`，异步清理旧物理文件 |
| GET 不带 `version_id` | 返回最新 normal 版本 |
| GET 带 `version_id` | 可选择禁止，或仅内部调试允许 |
| DELETE 不带 `version_id` | 删除当前对象，标记为 `purged`，提交后异步清理物理文件 |
| DELETE 带 `version_id` | 可选择返回参数错误，避免暴露历史版本概念 |
| ROLLBACK | 不支持 |

即使 disabled，也建议内部仍生成 `version_id`，不要使用空字符串作为唯一版本号。这样可以避免同一 key 多次覆盖时被唯一索引卡住，也方便审计和异步清理。

### enabled

`versioning=enabled` 表示保留历史版本：

| 操作 | 语义 |
|---|---|
| PUT | 创建一个新的 normal 版本并设为 latest，旧 latest 设为历史 |
| GET 不带 `version_id` | 返回 latest；如果 latest 是 delete marker，则返回 404 |
| GET 带 `version_id` | 返回指定 normal 版本；如果是 delete marker 或 purged，则返回 404 |
| HEAD/Metadata 不带 `version_id` | 返回 latest normal 的元数据；latest 是 delete marker 时返回 404 |
| ListObjects | 只列出 latest 且 status=normal 的对象 |
| ListObjectVersions | 列出 normal 和 delete marker 历史，默认不列 purged |
| DELETE 不带 `version_id` | 创建一个新的 delete marker 并设为 latest，不删除历史文件 |
| DELETE 带 `version_id` | 永久删除指定版本，必要时重新选择 latest |
| ROLLBACK | 从指定 normal 历史版本复制内容，创建一个新的 normal latest |

## 删除语义

### DELETE 不带 version_id

版本开启时，不应删除所有历史版本。正确行为是插入 delete marker：

```text
before:
v1 normal is_latest=0
v2 normal is_latest=1

DELETE object

after:
v1 normal        is_latest=0
v2 normal        is_latest=0
v3 delete_marker is_latest=1
```

影响：

| 项 | 变化 |
|---|---|
| 用户 GET 当前对象 | 404 |
| 历史版本 | 保留 |
| storage_size | 不变 |
| object_count | 如果删除前 latest 是 normal，则 -1 |
| 物理文件 | 不删除 |
| 事件 | 触发 `DeleteObject`，payload 标记 `delete_marker=true` |

### DELETE 带 version_id

这是永久删除指定版本：

```text
DELETE object?version_id=v2
```

处理规则：

| 被删版本 | 后续 latest 选择 | object_count 变化 |
|---|---|---|
| 历史 normal | latest 不变 | 0 |
| latest normal，前一个版本是 normal | 前一个 normal 成为 latest | 0 |
| latest normal，前一个版本是 delete marker 或不存在 | 前一个 delete marker 成为 latest，或无 latest | -1 |
| 历史 delete marker | latest 不变 | 0 |
| latest delete marker，前一个版本是 normal | 前一个 normal 成为 latest | +1 |
| latest delete marker，前一个版本也是 delete marker 或不存在 | 前一个 delete marker 成为 latest，或无 latest | 0 |

物理文件清理必须在数据库事务提交后执行。普通对象调用 `storage.Delete(storage_path)`，分片虚拟合并对象调用 `storage.DeleteParts(bucket, upload_id)`。

## 回滚语义

回滚不修改历史版本，也不把旧版本直接改成 latest。回滚必须创建新版本：

```text
before:
v1 normal content=A is_latest=0
v2 normal content=B is_latest=1

ROLLBACK to v1

after:
v1 normal content=A is_latest=0
v2 normal content=B is_latest=0
v3 normal content=A is_latest=1
```

这样可以保留时间线：

```text
T1 上传 A
T2 上传 B
T3 恢复到 A
```

而不是篡改成“v1 又变成了最新”。

### 回滚限制

| 条件 | 行为 |
|---|---|
| bucket versioning disabled | 返回不支持 |
| 源版本不存在 | 404 |
| 源版本是 delete marker | 返回参数错误 |
| 源版本已 purged | 404 |
| 当前 latest 已是源版本 | 仍建议创建新版本，保证操作可审计 |

### 回滚存储策略

默认策略是复制内容到新版本路径：

```text
storage.Copy(source_storage_path, bucket, object_key, new_version_id)
```

如果 Storage 抽象暂时没有 Copy 能力，可以用流式 `Get` + `Put` 实现。该 I/O 必须在数据库事务外执行。

事务失败时，需要删除刚创建的新物理文件。存储清理失败时记录日志，并交给异步清理任务重试。

## API 设计

现有接口继续保留：

| 方法 | 路径 | 语义 |
|---|---|---|
| PUT | `/api/v1/buckets/:bucket_name/objects/:object_key` | 上传或覆盖对象 |
| GET | `/api/v1/buckets/:bucket_name/objects/:object_key` | 读取 latest 或指定版本 |
| GET | `/api/v1/buckets/:bucket_name/objects/:object_key/metadata` | 读取元数据 |
| GET | `/api/v1/buckets/:bucket_name/objects/:object_key/versions` | 列出版本 |
| DELETE | `/api/v1/buckets/:bucket_name/objects/:object_key` | 删除当前对象或永久删除指定版本 |

新增回滚接口建议：

```text
POST /api/v1/buckets/:bucket_name/objects/:object_key/versions/:version_id/restore
```

请求：

```json
{
  "reason": "restore wrong upload"
}
```

响应：

```json
{
  "object_key": "dir/a.txt",
  "source_version_id": "old-version-id",
  "version_id": "new-version-id",
  "etag": "etag",
  "size": 1024
}
```

错误码建议：

| 场景 | 错误 |
|---|---|
| bucket 不存在 | `BucketNotFoundErr` |
| object/version 不存在 | `ResouceNotFoundErr` |
| versioning disabled | 新增 `VersioningDisabledErr` |
| 源版本是 delete marker | `ParamErr` |
| 并发冲突 | `ConflictErr` |

## 数据库设计

`objects` 表建议字段：

```sql
version_id VARCHAR(32) NOT NULL,
status TINYINT NOT NULL DEFAULT 1,
is_latest TINYINT NOT NULL DEFAULT 0,
deleted_at DATETIME NULL
```

建议索引：

```sql
UNIQUE KEY uk_bucket_key_ver (bucket_id, object_key_hash, version_id),
INDEX idx_bucket_key_latest (bucket_id, object_key_hash, is_latest),
INDEX idx_bucket_key_id (bucket_id, object_key_hash, id)
```

如果要在数据库层强约束唯一 latest，可以增加生成列：

```sql
latest_guard CHAR(32)
  GENERATED ALWAYS AS (
    CASE WHEN is_latest = 1 AND status <> 3 THEN object_key_hash ELSE NULL END
  ) STORED,
UNIQUE KEY uk_object_latest (bucket_id, latest_guard)
```

## Service 流程

### PUT

```text
1. 校验 bucket、权限、配额和 storage_class。
2. 生成 new_version_id。
3. 将请求体流式写入 Storage，得到 size、etag、storage_path。
4. 开启数据库事务。
5. 将同 key 当前 latest 置为 is_latest=0。
6. 插入新 normal 版本，is_latest=1。
7. 更新 bucket/user 统计和 metering。
8. 提交失败时删除第 3 步写入的新物理文件。
9. 提交成功后触发 PutObject 事件。
```

disabled 模式下，第 5 步之后还要把旧 normal 版本标记为 purged，并在提交后异步删除旧物理文件。

### DELETE current

```text
1. 校验 bucket 和权限。
2. 如果 versioning disabled，按永久删除当前版本处理。
3. 如果 versioning enabled，生成 delete_marker_version_id。
4. 开启数据库事务。
5. 将当前 latest 置为 is_latest=0。
6. 插入 delete marker，size=0，storage_path=NULL，is_latest=1。
7. 按可见对象数量更新 bucket/user/metering。
8. 提交成功后触发 DeleteObject 事件。
```

### DELETE version

```text
1. 查询目标版本，并记录其 storage_path 或 upload_id。
2. 开启数据库事务。
3. 将目标版本标记为 purged，deleted_at=now，is_latest=0。
4. 如果目标版本原本是 latest，按 id desc 找出下一个未 purged 版本设为 latest。
5. 更新 bucket/user/metering。
6. 提交成功后异步清理目标版本物理文件。
```

### ROLLBACK

```text
1. 查询源版本，必须 status=normal。
2. 复制源版本内容到 new_version_id 对应的新 storage_path。
3. 开启数据库事务。
4. 将当前 latest 置为 is_latest=0。
5. 插入新 normal 版本，内容元数据来自源版本，is_latest=1。
6. 更新 bucket/user/metering。
7. 提交失败时删除新复制出的物理文件。
8. 提交成功后触发 PutObject 或 RestoreObject 事件。
```

## Repo 能力建议

`IObjectRepo` 建议拆出更明确的方法，避免一个 `DeleteObject` 同时表达多种语义：

```go
type IObjectRepo interface {
    WithTx(tx tx.Tx) IObjectRepo
    CreateObject(ctx context.Context, object *do.CreateObject) (int64, error)
    CreateDeleteMarker(ctx context.Context, marker *do.CreateDeleteMarker) (int64, error)
    GetByKey(ctx context.Context, bucketName, objectKey, versionID string) (*do.ObjectDo, error)
    ListVersionsByKey(ctx context.Context, bucketName, objectKey string, includePurged bool) ([]*do.ObjectDo, error)
    MarkAllNotLatest(ctx context.Context, bucketID int64, objectKeyHash string) error
    MarkVersionPurged(ctx context.Context, bucketID int64, objectKeyHash, versionID string) (*do.ObjectDo, error)
    PromotePreviousVersion(ctx context.Context, bucketID int64, objectKeyHash string) (*do.ObjectDo, error)
}
```

关键点：

- Repo 层只返回 `repoerr`，不向 Service 暴露 GORM 错误。
- `ListVersions` 排序使用 `id desc` 或 `created_at desc`，不要按 UUID 类型的 `version_id desc`。
- 影响 latest 的方法必须在事务内调用。
- 缓存失效需要同时清理指定版本缓存和 latest 缓存。

## 并发控制

并发正确性优先由数据库事务和唯一约束保证。Redis 对象锁只能作为降低冲突的优化，不能作为唯一正确性来源。

同一个对象 key 的写操作包括：

```text
PUT
DELETE current
DELETE version
ROLLBACK
CompleteMultipartUpload
LifecycleExpiration
```

这些操作必须保证同一时刻不会留下多个 latest。建议：

- 写入 Storage 可先执行到临时版本路径，不持有数据库事务。
- 元数据切换在短事务内完成。
- 如果使用 Redis 锁，锁粒度应为 `bucketName/objectKey`，不要把 `version_id` 放入锁粒度。
- 大文件上传、下载和 Storage 长时间流式复制不应放在数据库事务内。

## 统计和计费

`bucket.object_count` 建议表示当前可见对象数，不表示历史版本数。

| 操作 | storage_size | object_count |
|---|---|---|
| PUT enabled，新建 key | +size | +1 |
| PUT enabled，覆盖已有 normal latest | +size | 0 |
| PUT enabled，覆盖 delete marker latest | +size | +1 |
| DELETE current enabled | 0 | normal latest 时 -1 |
| DELETE historical normal version | -size | 0 |
| DELETE latest normal 并提升 normal | -size | 0 |
| DELETE latest normal 且没有 normal 可提升 | -size | -1 |
| DELETE latest delete marker 并提升 normal | 0 | +1 |
| ROLLBACK 到 normal | +source_size | 当前不可见时 +1，否则 0 |

如果后续引入内容去重或引用计数，`storage_size` 可以改为按物理 blob 引用计费，但需要单独 ADR。

## 生命周期规则

生命周期过期删除需要区分：

| 规则 | 行为 |
|---|---|
| expiration_days | 对 latest normal 创建 delete marker，等价于 DELETE current |
| noncurrent_version_expiration_days | 永久删除非 latest normal 版本 |
| delete marker cleanup | 当 delete marker 后面没有 normal 历史版本时，可永久清理该 marker |

生命周期任务必须幂等。重复执行同一个过期事件时，不应重复扣减统计。

## 当前实现落地前需要修正

- `init.sql` 的 `buckets.versioning` 注释和默认值需要与 `consts.BucketVersioningDisabled/Enabled` 统一。
- `init.sql` 需要显式包含 `objects.is_latest` 字段，并和 GORM 生成模型一致。
- `DeleteObject` 不带 `version_id` 时不能删除所有版本，版本开启时必须创建 delete marker。
- `DeleteObject` 带 `version_id` 后需要重新选择 latest。
- `ListVersionsByFilter` 应按 `id desc` 或 `created_at desc` 排序。
- `GetObject` 不带 `version_id` 读到 delete marker 时应返回 404。
- 缓存失效需要包含 latest 缓存键。
- 回滚接口和 Service 方法需要新增。

## 验收标准

- 同一 bucket/object key 在非 purged 记录中最多一条 `is_latest=1`。
- PUT enabled 连续上传 3 次后，ListObjectVersions 返回 3 个 normal 版本，GET 不带版本返回第 3 个。
- DELETE current enabled 后，GET 不带版本返回 404，ListObjectVersions 可看到 delete marker 和历史版本。
- DELETE 指定历史版本后，物理文件在事务提交后被清理，latest 不变。
- DELETE 当前 delete marker 后，如果存在历史 normal 版本，该对象重新可见。
- ROLLBACK 到 v1 后产生 vN 新版本，v1 保持历史状态。
- 任何 DB 事务失败都不会留下最新元数据指向不存在的物理文件。
- Storage 清理失败不会回滚已提交元数据，但必须可重试。
