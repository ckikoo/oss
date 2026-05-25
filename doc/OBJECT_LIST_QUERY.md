# 对象列表查询增强

## API 参数

`GET /api/v1/buckets/:bucket_name/objects`

| 参数 | 说明 |
|---|---|
| `prefix` | 只返回指定前缀下的对象。 |
| `delimiter` | 模拟目录层级；命中的下一级目录返回到 `common_prefixes`。 |
| `marker` | 分页游标；首次请求为空，后续原样使用响应里的 `next_marker`。 |
| `max_keys` | 每页最多扫描对象数，默认和上限均为 1000。 |
| `storage_class` | 按存储类型过滤，支持 `STANDARD`、`IA`、`ARCHIVE`。 |
| `content_type` | 按 MIME 类型精确过滤。 |
| `created_at_start` | 创建时间起点，Unix 毫秒，闭区间。 |
| `created_at_end` | 创建时间终点，Unix 毫秒，闭区间。 |

响应新增：

| 字段 | 说明 |
|---|---|
| `items` | 当前页对象。 |
| `common_prefixes` | `delimiter` 折叠出的目录前缀。 |
| `next_marker` | 下一页游标，可能是内部游标，不保证等于对象 key。 |
| `is_truncated` | 是否还有下一页。 |
| `max_keys` | 本次实际使用的上限。 |

## 索引设计

`objects` 表按二级索引不超过 5 个收敛，保留列表查询需要的组合索引：

```sql
UNIQUE INDEX uk_bucket_key_ver(bucket_id, object_key_hash, version_id),
UNIQUE INDEX uk_object_latest(bucket_id, latest_guard),
INDEX idx_objects_key_lookup(bucket_name, object_key(255), version_id, is_latest, status),
INDEX idx_objects_list_key(bucket_name, is_latest, status, object_key(255)),
INDEX idx_objects_bucket_scan(bucket_id, status, is_latest, id)
```

设计目标：

- `bucket_name + is_latest + status` 固定收敛到当前可见对象。
- 使用 `object_key` keyset 游标配合 `ORDER BY object_key` 做分页，避免大 offset。
- `object_key LIKE 'prefix%'` 使用前缀索引范围扫描。
- `idx_objects_key_lookup` 覆盖对象元数据、下载、删除、版本恢复等按 key 查询路径。
- `idx_objects_bucket_scan` 覆盖 lifecycle 批量扫描路径。
- `storage_class`、`content_type`、`created_at` 先使用 `idx_objects_list_key` 做单页游标扫描后的残余过滤；如果真实压测证明瓶颈，再在 5 个索引预算内替换低价值索引。
- `delimiter` 存在时 Repo 返回当前层级条目，遇到子目录只返回一次 `common_prefixes`，并通过内部游标跳过该子树，避免深层目录对象撑满当前页。

## 压测记录

本轮先补充列表组装逻辑的 Go benchmark，数据库侧压测需要接入真实 MySQL 数据集后执行。

命令：

```bash
go test ./service/object -bench BenchmarkBuildListObjectsRespWithDelimiter -benchmem
```

结果：

```text
goos: linux
goarch: amd64
pkg: oss/service/object
cpu: 12th Gen Intel(R) Core(TM) i5-12490F
BenchmarkBuildListObjectsRespWithDelimiter-12  19596  56759 ns/op  35450 B/op  1019 allocs/op
```

数据库侧建议压测口径：

- 单 Bucket 100 万对象，`object_key` 均匀分布在 1000 个一级前缀下。
- 分别执行无过滤、`prefix`、`prefix+delimiter`、`storage_class`、`content_type`、`created_at_start/end`、组合过滤。
- 每组固定 `max_keys=1000`，循环使用 `next_marker` 翻 100 页。
- 记录 p50/p95/p99、`EXPLAIN` 使用索引、扫描行数和 DB CPU。
