# Storage 接口能力边界

本文梳理 `adaptor/storage` 的职责边界，作为后续多目录本地存储、S3/MinIO、阿里 OSS、腾讯 COS 等后端适配的接口依据。

## 当前结论

`adaptor/storage` 只负责对象二进制数据和派生资产的流式读写，不负责业务元数据、鉴权、配额、生命周期规则、事件投递和事务。

核心边界：

- Service 层负责 bucket、object key、version、upload id、part number 等业务语义。
- Storage 层负责把输入流落到后端，并返回 `storage_path`、`etag`、`sha256`、`size`。
- Storage I/O 必须在数据库事务外执行。
- Storage 删除失败不能回滚已提交元数据，需要由上层日志或异步清理补偿。
- Storage 接口不接收 Hertz/GORM/Redis 类型。

## 能力分组

### 普通对象能力

用于 `objects` 表中的普通对象版本。

接口：

```go
Put(ctx, bucket, objectKey, version, src)
Get(ctx, storagePath)
Delete(ctx, storagePath)
```

边界：

- `Put` 必须流式读取 `src`，禁止一次性读入大对象。
- `Get` 只根据已持久化的 `storage_path` 打开对象流。
- `Delete` 对不存在的对象应幂等成功。
- `storage_path` 是 Storage 返回给 Repo 元数据保存的后端定位符，上层不应自行拼接。

### Multipart 分片能力

用于 multipart upload 的临时分片和物理合并。

接口：

```go
PutPart(ctx, bucket, uploadID, partNumber, src)
DeletePart(ctx, bucket, uploadID, partNumber)
DeleteParts(ctx, bucket, uploadID)
MergeParts(ctx, bucket, objectKey, version, partPaths)
```

边界：

- 分片路径规则由 Storage 实现维护。
- `partPaths` 来自 `multipart_parts.storage_path`，调用方负责按 part number 排序和校验连续性。
- `MergeParts` 应按 `partPaths` 顺序流式合并。
- 虚拟合并对象仍可保留分片路径；物理合并成功后由上层决定是否清理分片。

### 派生资产能力

用于视频转码 HLS m3u8/ts/key 等派生文件，不进入普通 `objects` 表。

接口：

```go
PutAsset(ctx, bucket, assetKey, src)
GetAsset(ctx, bucket, assetKey)
DeleteAsset(ctx, bucket, assetKey)
DeleteAssetPrefix(ctx, bucket, prefix)
MoveAssetPrefix(ctx, bucket, srcPrefix, dstPrefix)
```

边界：

- `assetKey` 由视频业务层生成，例如 `_video/{transcode_id}/{profile}/index.m3u8`。
- 派生资产读写不能通过普通 `GetObject` 暴露。
- `MoveAssetPrefix` 用于 staging 发布，语义上是前缀级原子发布的最佳努力实现。
- 派生资产仍可以计入用户和 bucket 的存储用量，但计量由 Service/Repo 负责。

## 当前接口问题

### 1. `IStorage` 过大

当前 `IStorage` 同时包含普通对象、分片、视频派生资产能力。对本地实现没问题，但对云对象存储适配时会让实现一次性承载过多语义。

建议后续拆成：

```go
type IObjectStorage interface { ... }
type IMultipartStorage interface { ... }
type IAssetStorage interface { ... }
type IStorage interface {
    IObjectStorage
    IMultipartStorage
    IAssetStorage
}
```

短期可以保留 `IStorage` 组合接口，避免大范围改 Service 构造。

### 2. `BuildObjectPath` 泄露本地路径语义

`BuildObjectPath` 对 local 后端自然，但对 S3/MinIO/COS 这类后端只是 object key，不一定是文件系统路径。

建议：

- 新代码不要继续依赖 `BuildObjectPath`。
- 需要预估最终定位符时，改为由 `Put/MergeParts` 返回 `StoragePath`。
- 如果确实需要构造目标 key，应改名为 `BuildObjectLocator` 或沉到 local 私有实现。

### 3. 普通对象路径缺少安全校验

local 的派生资产路径已有 bucket/key 安全校验；普通对象和分片路径目前主要依赖业务层传入合法 bucket/object key。

建议：

- 后续为 local 普通对象和 multipart 路径补统一 key 清洗。
- 允许 object key 包含 `/` 作为层级，但禁止绝对路径、反斜杠路径逃逸、`.`、`..` 逃逸段。
- 对云存储后端保持 object key 原样语义，但仍在适配层拒绝明显危险输入。

### 4. 缺少后端级 Copy 能力

对象版本恢复和 CopyObject 当前可以用 `Get` + `Put` 流式实现。对本地和云后端都可行，但跨大对象复制会消耗服务端带宽。

建议后续增加可选能力：

```go
type IObjectCopyStorage interface {
    Copy(ctx, sourceStoragePath, dstBucket, dstObjectKey, dstVersion string) (*PutResult, error)
}
```

Service 可先 type assert，有则后端内复制，没有则回退 `Get` + `Put`。

## 后续适配原则

- `StoragePath` 统一表示后端定位符，不承诺是本地文件路径。
- 所有写入接口必须返回真实 `Size`、`Etag`，尽量返回 `Sha256`。
- 删除接口保持幂等。
- 大对象上传、下载、复制、合并必须流式处理。
- 后端临时失败不在 Storage 层吞掉，由 Service 记录日志或创建补偿任务。
- Storage 不维护 DB 状态，不自行更新对象、bucket、user 统计。
- Storage 不做业务鉴权，不理解 AccessKey、Policy、ACL。

## 推荐落地顺序

1. 保持现有 `IStorage` 不变，补本文档作为边界约束。
2. 为 local 普通对象和 multipart 路径补安全校验与测试。
3. 拆出 `IObjectStorage`、`IMultipartStorage`、`IAssetStorage` 子接口，但继续保留组合 `IStorage`。
4. 去除或重命名 `BuildObjectPath`，避免后续云后端适配受本地路径语义影响。
5. 增加可选 `Copy` 能力，优化同后端对象复制和版本恢复。
6. 再实现多目录 local、S3/MinIO、OSS/COS 等具体后端。

