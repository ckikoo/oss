---
name: oss-project-structure
user-invocable: true
description: >
  OSS 项目的架构规范和开发指南。
  当需要新增功能模块、理解项目分层结构、遵循命名规范时，使用此 skill。
  场景："如何添加新的存储类型"、"Object 模块如何扩展"、"项目架构是什么"。
applyTo:
  - "**/*.go"
---

# OSS 项目架构与规范 Skill

## 📐 项目整体架构

```
┌─────────────────────────────────────────────────────┐
│             HTTP Router (Hertz)                      │
│           router/*.go                                │
└──────────────┬──────────────────────────────────────┘
               │
┌──────────────▼──────────────────────────────────────┐
│         API Controllers (HTTP Layer)                 │
│       api/auth/*.go                                  │
│   (Handlers: CreateBucket, PutObject, etc.)         │
└──────────────┬──────────────────────────────────────┘
               │
┌──────────────▼──────────────────────────────────────┐
│       Service Layer (Business Logic)                 │
│    service/{module}/service.go                       │
│ (Orchestrate: repo, storage, error handling)        │
└──────────────┬──────────────────────────────────────┘
               │
       ┌───────┴────────┬─────────────┐
       │                │             │
┌──────▼────┐   ┌─────▼─────┐  ┌────▼──────┐
│Repository │   │  Storage  │  │   Redis   │
│  Layer    │   │   Layer   │  │   Cache   │
│           │   │           │  │           │
│ adaptor/  │   │ adaptor/  │  │ adaptor/  │
│ repo/     │   │ storage/  │  │ redis/    │
└───────────┘   └───────────┘  └───────────┘

       ├─ MySQL (via GORM)
       ├─ Local Disk / S3 / MinIO
       └─ Redis (locks, tokens, cache)
```

---

## 📦 分层说明

### 1️⃣ **Router 层** (`router/`)

**职责**: HTTP 路由注册、中间件链接

**关键文件**:
- `router.go` — 所有路由的统一注册点
- `auth.go` — 认证中间件（AK/SK）
- `acl.go` — 访问控制中间件
- `audit.go` — 操作日志中间件

**规范**:
```go
// 分组路由，逐级应用中间件
authGroup := h.Group("/api/v1", 
  NewAccessKeyMiddleware(adaptor),
  NewOperationLogMiddleware(adaptor))

bucketGroup := authGroup.Group("", 
  NewBucketACLMiddleware(adaptor))

bucketGroup.POST("/buckets", bucketCtrl.CreateBucket)
```

**要点**:
- 所有受保护端点都要通过 `NewAccessKeyMiddleware`
- 特定资源的 ACL 检查放在次级中间件
- 路由版本化（`/api/v1`）

---

### 2️⃣ **API 层** (`api/auth/`)

**职责**: HTTP 请求解析、响应序列化、参数校验

**关键文件**:
- `auth.go` — AccessKey 相关接口
- `bucket.go` — Bucket 操作接口
- `object.go` — Object 存储接口
- `multipart.go` — Multipart 上传接口
- `policy.go` — 权限策略接口
- `lifecycle.go` — 生命周期规则接口

**规范**:
```go
type CreateBucketReq struct {
  BucketName string `json:"bucket_name"` // 必填，小写下划线
  Acl        string `json:"acl"`         // 可选
}

func (ctrl *BucketCtrl) CreateBucket(ctx context.Context, c *app.RequestContext) {
  var req dto.CreateBucketReq
  if err := c.BindJSON(&req); err != nil {
    c.JSON(400, resp.NewErrResp(common.ParamErr))
    return
  }
  
  result, errno := ctrl.bucketService.CreateBucket(ctx, &req)
  if errno.NotOk() {
    c.JSON(errno.Code, resp.NewErrResp(errno))
    return
  }
  
  c.JSON(200, resp.NewSuccessResp(result))
}
```

**要点**:
- `UserInfoCtx` 从中间件注入，包含 `UserID`、`AccessKey` 等
- 参数校验在 handler 中进行
- 错误返回统一用 `common.Errno`
- Response 统一用 `resp.NewErrResp()`、`resp.NewSuccessResp()`

---

### 3️⃣ **Service 层** (`service/{module}/`)

**职责**: 业务逻辑编排、事务控制、多层交互

**关键模块**:
- `service/bucket/` — Bucket 生命周期管理
- `service/object/` — Object 存储和检索
- `service/multipart/` — Multipart 上传编排
- `service/policy/` — 权限策略应用
- `service/lifecycle/` — 生命周期规则执行
- `service/token/` — Token 生成和验证

**规范**:
```go
type Service struct {
  userRepo      admin.IUser           // 接口依赖注入
  objRepo       objectRepo.IObjectRepo
  bucketRepo    bucket.IBucketRepo
  storage       storage.IStorage      // 存储抽象
  db            *gorm.DB              // 事务用
}

// 构造函数：依赖从 Adaptor 获取
func NewService(adaptor adaptor.IAdaptor) *Service {
  return &Service{
    userRepo:   admin.NewUserRepo(adaptor),
    objRepo:    objectRepo.NewObjectRepo(adaptor),
    bucketRepo: bucket.NewBucketRepo(adaptor),
    storage:    adaptor.GetStorage(),
    db:         adaptor.GetDB(),
  }
}

// 业务方法：返回 (result, error) 或 common.Errno
func (srv *Service) PutObject(ctx *common.UserInfoCtx, req *dto.PutObjectReq, file *multipart.FileHeader) (*dto.PutObjectResp, common.Errno) {
  // 1. 参数校验
  if req.BucketName == "" {
    return nil, common.ParamErr.WithMsg("bucket_name required")
  }
  
  // 2. 业务逻辑
  bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, req.BucketName)
  if err != nil {
    return nil, common.DatabaseErr.WithErr(err)
  }
  
  // 3. 存储操作（使用接口，支持多种后端）
  f, err := file.Open()
  defer f.Close()
  putResult, err := srv.storage.Put(ctx, req.BucketName, req.ObjectKey, f)
  
  // 4. 数据库持久化 + 计量更新（事务）
  err = srv.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
    if err := srv.objRepo.CreateObjectWithTx(tx, createObj); err != nil {
      return err
    }
    return srv.userRepo.UpdateStorageUsedWithTx(tx, ctx.UserID, putResult.Size)
  })
  
  // 5. 错误转换和响应
  if err != nil {
    if errors.As(err, &errno) {
      return nil, errno
    }
    return nil, common.DatabaseErr.WithErr(err)
  }
  
  return &dto.PutObjectResp{...}, common.OK
}
```

**关键设计**:
- 依赖从 `Adaptor` 获取（便于测试和扩展）
- 所有 repo 操作用接口，支持多实现
- 事务管理在 service 层
- 错误处理使用 `common.Errno`
- 复杂操作要添加日志（zap.logger）

---

### 4️⃣ **Repository 层** (`adaptor/repo/`)

**职责**: 数据库 CRUD，隐藏 ORM 细节

**核心规范**:
```
每个模块（module）= {
  - i{module}.go         ← 接口定义
  - {module}_repo.go    ← 实现代码
}

示例：
  bucket/
  ├── ibucket.go        ← interface IBucketRepo
  └── bucket_repo.go    ← type BucketRepo struct
  
  object/
  ├── iobject.go        ← interface IObjectRepo
  └── object_repo.go    ← type ObjectRepo struct
```

**规范**:
```go
// ─ 接口定义 (ibucket.go) ─
type IBucketRepo interface {
  CreateBucket(ctx context.Context, bucket *do.CreateBucket) (*do.BucketDo, error)
  GetByName(ctx context.Context, userID int64, bucketName string) (*do.BucketDo, error)
  ListByUser(ctx context.Context, userID int64) ([]*do.BucketDo, error)
  UpdateBucketStatsWithTx(tx *gorm.DB, ctx context.Context, userID int64, bucketName string, objCount, size int64) error
}

// ─ 实现 (bucket_repo.go) ─
type BucketRepo struct {
  db    *gorm.DB
  cache redis.Client  // 可选
}

func (r *BucketRepo) GetByName(ctx context.Context, userID int64, bucketName string) (*do.BucketDo, error) {
  var bucket model.Bucket
  if err := r.db.WithContext(ctx).
    Where("user_id = ? AND bucket_name = ?", userID, bucketName).
    First(&bucket).Error; err != nil {
    return nil, err
  }
  return r.toDo(&bucket), nil
}
```

**要点**:
- 方法名统一：Create, Get, List, Update, Delete, DeleteXxxWithTx
- 使用 `context.Context` 传递用户信息和超时
- 分离接口和实现，便于 mock 测试
- 转换层（`toDo()`, `fromDo()`）隐藏模型细节

---

### 5️⃣ **Storage 层** (`adaptor/storage/`)

**职责**: 抽象物理存储，支持多种后端

**接口设计**:
```go
type IStorage interface {
  Put(ctx context.Context, bucket, key string, reader io.Reader) (*PutResult, error)
  Get(ctx context.Context, path string) (io.ReadCloser, error)
  Delete(ctx context.Context, path string) error
}

type PutResult struct {
  Size        int64
  Etag        string      // MD5 hash
  StoragePath string      // 物理路径（本地）或 key（S3）
}
```

**实现**:
- `adaptor/storage/local/` — 本地磁盘
- （可扩展）S3, MinIO 等

**使用**:
```go
// Service 中调用
putResult, err := srv.storage.Put(ctx, bucketName, objectKey, fileBody)
if err != nil {
  return nil, common.ServerErr.WithErr(err)
}

// 读取
reader, err := srv.storage.Get(ctx, storagePath)
defer reader.Close()
io.Copy(responseWriter, reader)
```

---

### 6️⃣ **Redis 层** (`adaptor/redis/`)

**职责**: 缓存、分布式锁、Token 存储

**关键模块**:
- `token.go` — Upload/Download Token 的生命周期管理
- `file.go` — 分布式文件锁（基于 bucket+object）
- `commonLocker.go` — 通用锁机制
- `multipart.go` — Multipart 超时管理（ZSet）

**规范**:
```go
// Token 管理（Hash 存储）
func (t *Token) CreateUploadToken(ctx context.Context, token string, req *dto.CreateUploadTokenReq, expire time.Duration) error {
  key := fmt.Sprintf("%s:upload:%s", consts.ServerName, token)
  _, err := t.rds.Pipelined(func(pipe redis.Pipeliner) error {
    pipe.HMSet(key, uploadTokenFields(req))
    pipe.Expire(key, expire)
    return nil
  })
  return err
}

// 文件锁（SET NX 原子操作）
func (l *FileLock) Lock(ctx context.Context, bucketName, objectKey string, expire time.Duration) (bool, error) {
  key := fmt.Sprintf("lock:%s:%s", bucketName, objectKey)
  ok, err := l.rds.SetNX(key, "1", expire).Result()
  return ok, err
}
```

---

## 🏗️ 数据模型分层

### DO (Domain Object) — `service/do/`

**用途**: Service 层内部使用，对应数据库表

**特点**:
- 字段完整，包含所有数据库列
- 使用指针类型处理 NULL（如 `*string`）
- 无序列化注解

```go
type CreateBucket struct {
  UserID    int64
  BucketName string
  Acl        string
  // ...
}

type BucketDo struct {
  ID          int64
  UserID      int64
  BucketName  string
  Acl         string
  CreatedAt   time.Time
  UpdatedAt   time.Time
}
```

### DTO (Data Transfer Object) — `service/dto/`

**用途**: HTTP API 的请求/响应序列化

**特点**:
- 只包含外部关心的字段
- 有 JSON/XML 序列化注解
- 可能包含验证标签（`validate:""`)

```go
type CreateBucketReq struct {
  BucketName string `json:"bucket_name" binding:"required"`
  Acl        string `json:"acl"`
}

type BucketResp struct {
  BucketName  string `json:"bucket_name"`
  Acl         string `json:"acl"`
  CreatedAt   int64  `json:"created_at"`  // Unix timestamp
  UpdatedAt   int64  `json:"updated_at"`
}
```

### Model — `adaptor/repo/model/` (自动生成)

**用途**: GORM 模型，由 `gorm/gen` 自动生成

**特点**:
- 对应数据库表结构
- 包含 GORM 标签（`gorm:"column:xxx"`)
- 不应手动修改，使用 `gentool` 重新生成

```go
type Bucket struct {
  ID         int64     `gorm:"primaryKey"`
  UserID     int64     `gorm:"column:user_id"`
  BucketName string    `gorm:"column:bucket_name"`
  Acl        string    `gorm:"column:acl"`
  CreatedAt  time.Time `gorm:"column:created_at"`
}
```

---

## 📝 命名规范

| 元素 | 规范 | 示例 |
|------|------|------|
| **Interface** | 首字母大写，`I` 前缀 | `IBucketRepo`, `IStorage`, `IToken` |
| **Struct** | 首字母大写，名词性 | `BucketRepo`, `Service`, `Token` |
| **Function** | 首字母大写（exported），动词性 | `CreateBucket`, `GetByName`, `ListByUser` |
| **Constant** | 全大写下划线 | `StorageClassStandard`, `ACLPrivate`, `MaxUploadSize` |
| **Package** | 小写（无下划线） | `bucket`, `object`, `admin` |
| **Variables** | 驼峰命名 | `bucketRepo`, `userID`, `errorCode` |
| **JSON Field** | 小写下划线 | `bucket_name`, `storage_class`, `upload_id` |

---

## 🔄 常见开发流程

### ➕ 新增功能模块

以"审计日志"为例：

```
1. 数据库模型 (init.sql)
   CREATE TABLE audits (
     id BIGINT PRIMARY KEY,
     user_id BIGINT,
     action VARCHAR(255),
     ...
   );

2. 生成 ORM 代码
   cd adaptor/repo && gentool -c gen.yaml

3. 创建 Repository 层
   adaptor/repo/audit/
   ├── iaudit.go          ← interface IAuditRepo
   └── audit_repo.go      ← implement CreateAudit, ListByUser, etc.

4. 创建数据对象
   service/do/audit.go    ← CreateAudit struct, AuditDo struct
   service/dto/audit.go   ← AuditResp struct

5. 创建 Service
   service/audit/service.go
   └── ListAudits(ctx, userID, filter) → []*dto.AuditResp

6. 创建 Controller/Handler
   api/auth/audit.go
   └── ListAudits handler → GET /api/v1/logs

7. 注册路由
   router/router.go
   └── authGroup.GET("/logs", auditCtrl.ListAudits)
```

### 🔧 扩展存储后端

现有存储接口：`adaptor/storage/istorage.go`

```go
type IStorage interface {
  Put(ctx context.Context, bucket, key string, reader io.Reader) (*PutResult, error)
  Get(ctx context.Context, path string) (io.ReadCloser, error)
  Delete(ctx context.Context, path string) error
}
```

**新增 S3 支持**:
```
1. 创建实现
   adaptor/storage/s3/
   └── s3.go        ← type S3Storage struct, 实现 IStorage

2. 在 Adaptor 中配置选择
   adaptor/adaptor.go
   └── func (a *Adaptor) GetStorage() → 根据配置选择 local 或 s3

3. Service 无需修改，自动使用新实现
```

### ✅ 添加校验和错误处理

```go
// 1. 定义新的 Errno（common/errno.go）
var (
  CustomErr = Errno{Code: 12000, Msg: "Custom Error"}
)

// 2. 在 Service 中使用
func (srv *Service) DoSomething(ctx context.Context) (result, common.Errno) {
  if condition {
    return nil, CustomErr.WithMsg("additional info")
  }
  return result, common.OK
}

// 3. Handler 自动处理
if errno.NotOk() {
  c.JSON(errno.Code, resp.NewErrResp(errno))
  return
}
```

---

## 🎯 关键约定

### 事务管理
- **创建/删除**: 必须使用事务确保原子性
- **查询**: 单表查询无需事务，多表 JOIN 按需添加事务

```go
err := srv.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
  if err := step1(tx); err != nil {
    return err
  }
  return step2(tx)
})
```

### 错误处理
- 所有方法返回 `common.Errno` 或 `error`
- Service 返回 `(result, common.Errno)`
- Repo 返回 `error`（数据库错误由 Service 转化）

```go
// Repo 返回原始错误
obj, err := r.objRepo.GetByKey(ctx, bucketName, objectKey)
if err != nil {
  return nil, common.DatabaseErr.WithErr(err)  // ← 转化
}
```

### 上下文传递
- 所有 I/O 操作必须传递 `context.Context`
- Service 层使用 `*common.UserInfoCtx`（包含 UserID）
- Repo 和 Storage 使用 `context.Context`

```go
func (srv *Service) ListObjects(ctx *common.UserInfoCtx, req *dto.ListObjectsReq) (..., common.Errno) {
  // ctx 包含 ctx.UserID, ctx.AccessKey 等
  objects, err := srv.objRepo.ListByUser(ctx, ctx.UserID)
}
```

### 日志记录
- 使用 `zap` logger
- ERROR 级别记录异常情况
- DEBUG 级别记录关键业务步骤

```go
import "go.uber.org/zap"

logger.Error("failed to delete object", zap.String("key", key), zap.Error(err))
logger.Debug("object deleted successfully", zap.String("key", key))
```

---

## 📋 文件位置速查表

| 功能 | 文件 | 职责 |
|------|------|------|
| 路由注册 | `router/router.go` | 所有路由的统一入口 |
| HTTP 处理 | `api/auth/{module}.go` | 请求解析、响应序列化 |
| 业务逻辑 | `service/{module}/service.go` | 编排、事务、计量 |
| 数据访问 | `adaptor/repo/{module}/{module}_repo.go` | CRUD 操作 |
| 接口定义 | `adaptor/repo/{module}/i{module}.go` | 约定 |
| 模型定义 | `adaptor/repo/model/*.gen.go` | ORM 模型（自动生成） |
| 数据对象 | `service/do/{module}.go` | 内部数据结构 |
| 数据传输 | `service/dto/{module}.go` | API 请求/响应 |
| 存储抽象 | `adaptor/storage/` | 物理存储 |
| Redis 操作 | `adaptor/redis/{module}.go` | 缓存和锁 |
| 配置管理 | `config/config.go` | 环境变量和配置文件 |
| 常量定义 | `consts/consts.go` | 系统常量 |
| 错误定义 | `common/errno.go` | 错误码和消息 |

---

## 🚀 快速检查清单

新增功能时：
- [ ] 数据库表和初始化 SQL 已添加？
- [ ] 运行 `gentool -c gen.yaml` 生成模型和查询代码？
- [ ] Repository interface 和实现分离？
- [ ] Service 使用接口依赖注入？
- [ ] Handler 和路由已注册？
- [ ] 错误处理使用 `common.Errno`？
- [ ] 涉及多步操作的使用了事务？
- [ ] 所有 I/O 操作传递了 `context`？
- [ ] 关键业务逻辑添加了日志？
- [ ] API 的请求/响应用了 DTO？

