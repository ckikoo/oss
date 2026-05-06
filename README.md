# OSS 服务

基于 **Hertz** 和 **GORM** 的对象存储服务后端。

## 技术栈

- **框架**: [Hertz](https://github.com/cloudwego/hertz)
- **ORM**: [GORM](https://gorm.io/)
- **代码生成**: [GORM Gen](https://gorm.io/gen/)
- **数据库**: MySQL
- **缓存**: Redis
- **对象转换**: [Govertor](https://github.com/jmattheis/goverter)

## 功能特性

- **AK/SK 认证**: 支持 Access Key 和 Secret Key 认证，保护所有 bucket、object 和 multipart API。
- **Bucket 管理**: 创建、列出、获取、更新和删除 bucket。
- **Object 存储**: 上传、下载、获取元数据和删除对象。
- **Multipart Upload**: 支持分片上传，实现大文件上传。
- **权限控制**: 基于 JSON 的细粒度权限系统，支持 bucket policy 及多维策略规则。
- **策略查询优化**: `bucket_policies` 查询使用 `utils/pool` 控制并发加载子表，避免 N+1 查询卡顿。
- **版本控制**: 支持对象版本管理。
- **存储类型**: 支持 STANDARD、IA、ARCHIVE 存储类。
- **分布式锁**: 基于 Redis 的文件锁机制，支持并发控制和原子操作。

> 更多项目结构、模块说明和文件索引请参见 [PROJECT_INDEX.md](PROJECT_INDEX.md)。

## API 认证

所有 bucket、object 和 multipart 相关的 API 都需要 AK/SK 认证：

- **Authorization 方式**: 
  - `Authorization: OSS <access_key>:<timestamp>:<signature>`
  - `<timestamp>` 用于防重放，签名过期范围约为 30 秒
- 对于 `application/octet-stream` 和 `multipart/*` 请求，签名计算时会跳过 body 内容

## Bucket Policy API

权限策略相关接口目前支持 bucket 级别策略管理。详细文档请参见 [POLICY_API.md](POLICY_API.md)。

### 创建 Bucket Policy
- `POST /api/v1/buckets/:bucket_name/policies`
- 受 AK/SK 认证保护

### 列表 Bucket Policy
- `GET /api/v1/buckets/:bucket_name/policies`
- 受 AK/SK 认证保护

### 实现说明
- `ListBucketPolicies` 先读取 `bucket_policies` 头表
- 再并发加载 `policy_principals`、`policy_actions`、`policy_resources` 和 `policy_conditions` 子表
- 使用 `oss/utils/pool` 控制并发数量，避免策略数量多时出现过多数据库连接或查询压力

## Multipart Upload

支持大文件分片上传，使用虚拟合并策略实现高性能。详细实现请参考 [MULTIPART_GUIDE.md](MULTIPART_GUIDE.md)。

### 上传流程

1. **初始化**: `POST /api/v1/buckets/{bucket}/multipart/uploads`
2. **上传分片**: `PUT /api/v1/buckets/{bucket}/multipart/uploads/{upload_id}/parts/{part_number}`
   - 直接发送分片二进制 body
   - 需携带 `Content-MD5` 头进行校验
3. **完成上传**: `POST /api/v1/buckets/{bucket}/multipart/uploads/{upload_id}/complete`
4. **中止上传**: `DELETE /api/v1/buckets/{bucket}/multipart/uploads/{upload_id}`

### 特性

- 虚拟合并，无需物理文件组装
- 流式上传，避免内存溢出
- 分片独立存储，便于管理与重传
- 自动超时清理

## Metering／日统计

项目已实现日级流量与请求类型统计，数据写入 `metering_daily` 表。统计包括：

- `storage_size`：当前日期内对象存储量增减（上传增加、删除减少）
- `object_count`：对象数量变更（上传 +1、删除 -1）
- `upload_flow`：PUT/上传流量，按对象大小累加
- `download_flow`：GET/下载流量，按实际传输字节累加（使用 `io.MultiWriter` 流式写出时实时计数）
- `get_request_count`：GET 请求次数
- `put_request_count`：PUT 请求次数
- `del_request_count`：DELETE 请求次数

统计同时支持 bucket 级和用户总计两种粒度：

- `bucket_id` 有值时表示该 bucket 的明细统计
- `bucket_id` 为 NULL 时表示用户级总计统计

当前接口查询入口为：

- `GET /api/v1/metrics/daily`

支持的查询参数：

- `user_id`：用户 ID
- `bucket_id`：bucket ID
- `date_from`：起始统计日期，格式 `YYYY-MM-DD`
- `date_to`：结束统计日期，格式 `YYYY-MM-DD`

该接口返回按天汇总的统计条目，适用于日常流量审计、费用核算和用户行为分析。

> 注意：对象下载流量统计以实际传输字节为准，避免仅依赖对象元数据 `Size`，已通过 `io.MultiWriter` 统计真实下行流量。

## 分布式锁机制

项目实现了基于 Redis 的分布式文件锁机制，用于控制对同一文件对象的并发访问。

### 锁特性

- **原子性**: 使用 Redis SET NX 命令确保只有一个客户端能获取锁
- **安全性**: 使用 Lua 脚本确保只有锁的持有者才能释放锁
- **自动过期**: 锁会自动过期，避免死锁
- **可续期**: 支持锁的续期操作
- **高性能**: 基于内存的 Redis 操作

### 锁接口

```go
type IFileLock interface {
    // 获取锁
    AcquireLock(ctx context.Context, bucketName string, objectName string, uuid string, ttl time.Duration) (bool, error)
    // 释放锁
    ReleaseLock(ctx context.Context, bucketName string, objectName string, uuid string) error
    // 刷新锁
    RefreshLock(ctx context.Context, bucketName string, objectName string, uuid string, ttl time.Duration) (bool, error)
    // 检查锁状态
    CheckLock(ctx context.Context, bucketName string, objectName string, uuid string) (bool, error)
    // 强制释放锁（管理员操作）
    ForceReleaseLock(ctx context.Context, bucketName string, objectName string) error
}
```

### 锁 Key 格式

锁的 Redis Key 格式为: `{ServerName}:lock:file:{bucketName}:{objectName}`

### 使用示例

```go
// 获取锁
lockID := "unique-uuid"
success, err := fileLock.AcquireLock(ctx, "mybucket", "myobject.txt", lockID, 30*time.Second)

// 释放锁
err = fileLock.ReleaseLock(ctx, "mybucket", "myobject.txt", lockID)

// 刷新锁
success, err = fileLock.RefreshLock(ctx, "mybucket", "myobject.txt", lockID, 30*time.Second)
```

## 快速开始

### 1. 环境要求

- Go 1.25+
- MySQL 8.0+
- Redis

### 2. 下载依赖

```bash
go mod tidy
```

### 3. 配置

当前项目默认读取 `config/config.go` 中的 `config.yaml`：

```bash
# 编辑 config.yaml，设置 MySQL 和 Redis 连接信息
```

如果需要使用自定义配置文件路径，可在启动时传入 `-c` 参数：

```bash
go run ./main.go -c /path/to/config.yaml
```

### 4. 初始化数据库

```bash
mysql -uroot -p < init.sql
```

### 5. 生成代码

```bash
# 生成 GORM Model 和 Query
go run ./tools/gen.go

# 生成对象转换器
goverter gen ./service
```

### 6. 运行服务

```bash
go run ./main.go
```

服务默认启动在 `http://localhost:8080`。

## 存储类型常量

项目中对 `storage_class` 的默认值均已统一使用常量定义，避免字符串硬编码：

- `consts.StorageClassStandard`
- `consts.StorageClassIA`
- `consts.StorageClassArchive`

默认对象和 Bucket 的 `storage_class` 均会回退到 `StorageClassStandard`。

## 项目结构

更多项目结构、模块说明与详细索引请参见 [PROJECT_INDEX.md](PROJECT_INDEX.md)。

## 关键命令

```bash
# 生成数据库模型和查询代码
go run ./tools/gen.go

# 生成对象转换器
goverter gen ./service

# 启动服务
go run ./main.go

# 构建二进制
go build -o oss ./main.go
```

## 架构说明

### 分层架构

1. **main.go**: 应用入口，负责服务启动和依赖注入
2. **adaptor**: 适配器层，封装外部依赖 (DB、Redis) 和数据访问实现
3. **service**: 业务逻辑层，包含领域对象、视图对象和转换器
4. **config**: 配置管理
5. **common**: 通用工具和中间件

### 数据流

```
HTTP Request -> Handler -> Service -> Adaptor/Repo -> Database
                      -> VO     -> Converter -> DO
```

### 代码生成

- **GORM Gen**: 根据数据库表自动生成 Model 和 Query 方法
- **Govertor**: 根据接口定义生成类型安全对象转换器

## 使用示例

### 创建用户

```go
userRepo := user.NewUser(adaptor)
userID, err := userRepo.CreateUser(ctx, &do.CreateUser{
    Email:        "user@example.com",
    StorageQuota: 100 * 1024 * 1024 * 1024,
})
```

### 对象转换

```go
converter := converter.NewConverter()
userVO := converter.UserToVO(userDO)
```

## 开发指南

### 添加新功能

1. 在 `adaptor/repo/model` 中添加数据库结构并更新 `init.sql`
2. 运行 `go run ./tools/gen.go` 生成 GORM Model 和 Query
3. 在 `service/do` 中定义领域对象
4. 在 `service/vo` 中定义视图对象
5. 在 `service/converter` 中定义转换接口
6. 运行 `goverter gen ./service` 生成转换器
7. 在 `adaptor/repo` 中实现数据访问逻辑
8. 在 `service` 中实现业务逻辑
9. 在 `main.go` 或 `route` 中添加路由

### 数据库迁移

修改 `init.sql` 并重新执行初始化脚本。

## 部署

```bash
go build -o oss ./main.go
./oss
```

## 关键命令

```bash
# 安装工具
make install-tools

# 生成数据库模型和查询代码
make gen

# 运行服务
make run

# 构建二进制
make build

# 生成对象转换器
goverter gen ./service
```

## 架构说明

### 分层架构

1. **cmd**: 应用入口，负责服务启动和依赖注入
2. **adaptor**: 适配器层，封装外部依赖 (DB, Redis, 第三方服务)
3. **service**: 业务逻辑层，包含领域对象、视图对象和转换器
4. **config**: 配置管理
5. **common**: 通用工具和中间件

### 数据流

```
HTTP Request -> Handler -> Service -> Adaptor/Repo -> Database
                      -> VO     -> Converter -> DO
```

### 代码生成

- **GORM Gen**: 根据数据库表自动生成 Model 和 Query 方法
- **Govertor**: 根据接口定义生成类型安全的对象转换器

## 使用示例

### 创建用户

```go
// 业务层调用
userRepo := user.NewUser(adaptor)
userID, err := userRepo.CreateUser(ctx, &do.CreateUser{
    Email:        "user@example.com",
    StorageQuota: 100 * 1024 * 1024 * 1024, // 100GB
})

// 查询用户
userInfo, err := userRepo.GetUserInfoById(ctx, userID)
```

### 对象转换

```go
// 使用生成的转换器
converter := converter.NewConverter()
userVO := converter.UserToVO(userDO)
```

## 开发指南

### 添加新功能

1. 在 `adaptor/repo/model` 中定义数据库表结构
2. 运行 `make gen` 生成 Model 和 Query
3. 在 `service/do` 中定义领域对象
4. 在 `service/vo` 中定义视图对象
5. 在 `service/converter` 中定义转换接口
6. 运行 `goverter gen ./service` 生成转换器
7. 在 `adaptor/repo` 中实现数据访问逻辑
8. 在 `service` 中实现业务逻辑
9. 在 `cmd/server` 中添加路由和依赖注入

### 数据库迁移

修改 `init.sql` 并重新运行初始化脚本。

## 部署

```bash
# 构建
make build

# 运行
./oss
```

## 许可证

[MIT License](LICENSE)
