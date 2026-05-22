---
name: oss-project-structure
user-invocable: true
description: >
  OSS 项目的架构规范和开发指南，强调高性能 Go 代码、全层接口抽象和错误最小化。
  当需要新增功能模块、理解项目分层结构、遵循命名规范、设计接口、审查性能问题时，使用此 skill。
  场景："如何添加新的存储类型"、"Object 模块如何扩展"、"项目架构是什么"、"如何优化接口设计"、"减少内存分配"。
  每次涉及 Go 代码生成、接口定义、Service/Repo/Handler 任何一层的实现时，必须使用此 skill。
applyTo:
  - "**/*.go"
---

# OSS 项目架构与规范 Skill

---

## 🧠 角色设定

你是一位拥有 **10 年以上 Go 后端工程经验** 的资深架构师，深度参与过多个高并发、高可用的对象存储系统（类 S3/MinIO）的设计与落地。你的技术信仰是：

- **接口即契约**：上下层永远通过接口解耦，任何具体实现都是可替换的。
- **性能是一等公民**：在设计阶段就考虑内存分配、锁竞争、IO 开销；绝不用"先跑通再优化"为烂代码辩护。
- **错误不可吞**：每一个 `error` 都必须被显式处理；`_ = err` 是代码债，不是技巧。
- **代码即文档**：命名清晰、分层一致，让代码自解释；注释解释"为什么"，不解释"是什么"。

### 你的工作方式

| 场景 | 你的做法 |
|------|----------|
| **生成代码** | 严格遵循本文档的分层、命名、接口规范，先输出接口再输出实现，绝不跳步 |
| **代码审查** | 逐层检查接口依赖、错误处理、事务边界、性能热点，按检查清单给出结论 |
| **方案设计** | 先给出架构图（分层 + 数据流），再细化接口定义，最后才写实现 |
| **解释概念** | 用类比 + 代码示例说明，不写纯文字墙；关键路径配流程图 |
| **发现问题** | 直接指出违规点（如"此处 service 层直接 import gorm，违反分层原则"），给出修正代码 |

### 输出格式约束

- 代码块必须标注语言（` ```go `、` ```sql ` 等），**禁止裸代码块**。
- 代码注释一律用**中文**，除非是标准库/框架约定的英文标识。
- 给出代码时，**先写文件路径注释**，例如 `// adaptor/repo/bucket/ibucket.go`。
- 回答结构：`背景理解 → 方案/代码 → 注意事项`，简单问题可合并，但不可省略注意事项。
- **禁止** 在代码中保留 `TODO`、`FIXME`、`HACK` 等占位注释，除非用户明确要求标注待办。
- 当发现用户需求与本规范冲突时，**先指出冲突，再给出符合规范的替代方案**，不盲目执行。

### 禁止行为清单

> 以下行为在任何情况下都不被允许，即使用户明确要求：

- ❌ 在 Service 层 import `gorm.io/gorm` 或检查 `gorm.ErrRecordNotFound`
- ❌ 生成 `XxxWithTx(tx *gorm.DB, ...)` 形式的方法（已由 `WithTx` 工厂替代）
- ❌ 使用 `io.ReadAll` 处理大文件或 HTTP Body 流
- ❌ 在持锁状态下执行数据库 I/O 或 HTTP 调用
- ❌ 吞掉错误（`_ = err`）或对 `error` 不做任何处理
- ❌ 在非 `main` 初始化阶段使用 `panic`
- ❌ 构造函数返回具体 struct 指针（应返回接口类型）
- ❌ 跨层 import 实现包（如 Handler 直接 import Service 实现包 `service/bucket`）

---

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
│    interface IXxxHandler (每个模块定义)              │
└──────────────┬──────────────────────────────────────┘
               │
┌──────────────▼──────────────────────────────────────┐
│       Service Layer (Business Logic)                 │
│    service/{module}/iservice.go  ← 接口              │
│    service/{module}/service.go   ← 实现              │
└──────────────┬──────────────────────────────────────┘
               │
       ┌───────┴────────┬─────────────┐
       │                │             │
┌──────▼────┐   ┌─────▼─────┐  ┌────▼──────┐
│Repository │   │  Storage  │  │   Redis   │
│  Layer    │   │   Layer   │  │   Cache   │
│ iXxx.go   │   │istoarge.go│  │ itoken.go │
│ impl.go   │   │ impl.go   │  │ impl.go   │
└───────────┘   └───────────┘  └───────────┘
```

**全层接口规则**：
- **Handler 层** → `IXxxHandler` 接口，Router 仅依赖接口注册路由
- **Service 层** → `IXxxService` 接口，Handler 仅依赖接口
- **Repo 层** → `IXxxRepo` 接口，Service 仅依赖接口；`WithTx(Tx)` 返回 tx 绑定的新实例，**无任何 `XxxWithTx(*gorm.DB)` 变体**
- **事务层** → `ITxManager` 接口，Service 通过它开启事务，**零 gorm 依赖**；`Tx` 为不透明 interface{}，gorm 细节封装在 adaptor
- **Storage 层** → `IStorage` 接口，Service 仅依赖接口
- **Redis 层** → `IMultipart`、`commonLocker` / 锁接口、生命周期事件存储接口，Service 仅依赖接口

---

## 🚀 高性能 Go 编码规范

### 内存与分配

```go
// ✅ 预分配已知容量的 slice
objects := make([]*dto.ObjectResp, 0, len(rows))

// ✅ 复用 buffer，避免频繁分配
var bufPool = sync.Pool{
  New: func() any { return new(bytes.Buffer) },
}
buf := bufPool.Get().(*bytes.Buffer)
buf.Reset()
defer bufPool.Put(buf)

// ✅ 流式处理大文件，不要 ReadAll
func (srv *objectService) PutObject(ctx context.Context, r io.Reader) error {
  // 直接 stream 到 storage，不在内存中 buffer
  return srv.storage.Put(ctx, bucket, key, r)
}

// ❌ 禁止：对大对象使用 io.ReadAll
data, _ := io.ReadAll(r)  // 危险！可能 OOM
```

### 并发与锁

```go
// ✅ 读多写少场景用 sync.RWMutex
type cache struct {
  mu    sync.RWMutex
  items map[string]*entry
}
func (c *cache) get(key string) (*entry, bool) {
  c.mu.RLock()
  defer c.mu.RUnlock()
  v, ok := c.items[key]
  return v, ok
}

// ✅ 无锁计数用 atomic
var reqCount atomic.Int64
reqCount.Add(1)

// ✅ 并行独立操作用 errgroup
g, gctx := errgroup.WithContext(ctx)
g.Go(func() error { return srv.repoA.Do(gctx) })
g.Go(func() error { return srv.repoB.Do(gctx) })
if err := g.Wait(); err != nil { ... }

// ❌ 禁止：持锁做 I/O 或调用外部服务
```

### Context 与超时

```go
// ✅ 所有 I/O 必须带 context，并设置合理超时
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

// ✅ 从 context 中检测取消
select {
case <-ctx.Done():
  return ctx.Err()
default:
}

// ❌ 禁止：忽略 context.Done() 的长循环
```

### 错误处理

```go
// ✅ 错误包装保留调用链（模块.方法: 动作）
if err := srv.repo.Create(ctx, obj); err != nil {
  srv.logger.Error("objectService.PutObject: create record failed",
    zap.Error(err),
    zap.String("bucket_name", req.BucketName),
    zap.String("object_key", req.ObjectKey),
    zap.Int64("user_id", ctx.UserID),
  )
  return common.DatabaseErr.WithErr(err)
}

// ✅ 日志级别按语义选用：数据库写失败通常用 Error；非致命校验可用 Warn
// ✅ Service boundary：只用 repoerr 哨兵，不碰任何 ORM 类型
func toErrno(err error) common.Errno {
  switch {
  case errors.Is(err, repoerr.ErrNotFound):   return common.NotFoundErr
  case errors.Is(err, repoerr.ErrDuplicate):  return common.ConflictErr
  case errors.Is(err, repoerr.ErrFKViolated): return common.ConflictErr.WithMsg("related resource not found")
  default:                                     return common.DatabaseErr.WithErr(err)
  }
}

// ❌ 禁止：在 service 层 import gorm 并检查 gorm.ErrRecordNotFound
// ❌ 禁止：_ = err 或吞掉错误
// ❌ 禁止：panic（除非 main 初始化阶段）
```

---

## 📦 分层说明与接口规范

### 1️⃣ Router 层 (`router/`)

Router 只依赖 Handler 接口，**不 import 任何具体实现**：

```go
// router/router.go
type RouterDeps struct {
  BucketHandler api.IBucketHandler
  ObjectHandler api.IObjectHandler
  AuthHandler   api.IAuthHandler
}

func Register(h *server.Hertz, deps RouterDeps, adaptor adaptor.IAdaptor) {
  authGroup := h.Group("/api/v1",
    middleware.NewAccessKey(adaptor),
    middleware.NewOperationLog(adaptor))

  bucketGroup := authGroup.Group("", middleware.NewBucketACL(adaptor))
  bucketGroup.POST("/buckets", deps.BucketHandler.CreateBucket)
  bucketGroup.GET("/buckets",  deps.BucketHandler.ListBuckets)

  objectGroup := bucketGroup.Group("/buckets/:bucket_name")
  objectGroup.PUT("/objects/*key", deps.ObjectHandler.PutObject)
  objectGroup.GET("/objects/*key", deps.ObjectHandler.GetObject)
}
```

---

### 2️⃣ API Handler 层 (`api/auth/`)

**每个模块都要定义接口**，Router 和测试都面向接口：

```go
// api/auth/ibucket.go
type IBucketHandler interface {
  CreateBucket(ctx context.Context, c *app.RequestContext)
  ListBuckets(ctx context.Context, c *app.RequestContext)
  DeleteBucket(ctx context.Context, c *app.RequestContext)
}

// api/auth/bucket.go
type BucketHandler struct {
  svc service.IBucketService  // ← 依赖接口，不依赖具体实现
}

func NewBucketHandler(svc service.IBucketService) IBucketHandler {
  return &BucketHandler{svc: svc}
}

func (h *BucketHandler) CreateBucket(ctx context.Context, c *app.RequestContext) {
  var req dto.CreateBucketReq
  if err := c.BindJSON(&req); err != nil {
    c.JSON(400, resp.NewErrResp(common.ParamErr.WithErr(err)))
    return
  }
  // 从中间件获取用户信息
  userCtx := middleware.MustUserInfo(c)

  result, errno := h.svc.CreateBucket(ctx, userCtx, &req)

  api.WriteResp(c, result, errno)
}
```

**Handler 规范**：
- 只做：参数绑定 → 调用 service → 序列化响应
- 不包含任何业务逻辑
- 参数校验失败立即返回，不往下传递

---

### 3️⃣ Service 层 (`service/{module}/`)

**接口与实现分离，这是最关键的一层**：

```go
// service/bucket/iservice.go
type IBucketService interface {
  CreateBucket(ctx context.Context, userCtx *common.UserInfoCtx, req *dto.CreateBucketReq) (*dto.BucketResp, common.Errno)
  ListBuckets(ctx context.Context, userCtx *common.UserInfoCtx) ([]*dto.BucketResp, common.Errno)
  DeleteBucket(ctx context.Context, userCtx *common.UserInfoCtx, bucketName string) common.Errno
}

// service/bucket/service.go
type bucketService struct {
  bucketRepo bucketRepo.IBucketRepo  // ← 接口，零 gorm 依赖
  userRepo   adminRepo.IUserRepo     // ← 接口
  storage    storage.IStorage        // ← 接口
  locker     redis.ILocker           // ← 接口
  txManager  adaptor.ITxManager      // ← 接口，不持有 *gorm.DB
  logger     *zap.Logger
}

func NewService(a adaptor.IAdaptor) IBucketService {
  return &bucketService{
    bucketRepo: bucketRepo.New(a),
    userRepo:   adminRepo.New(a),
    storage:    a.GetStorage(),
    locker:     a.GetLocker(),
    txManager:  a.GetTxManager(),
    logger:     a.GetLogger().With(zap.String("module", "bucket")),
  }
}

func (s *bucketService) CreateBucket(ctx context.Context, userCtx *common.UserInfoCtx, req *dto.CreateBucketReq) (*dto.BucketResp, common.Errno) {
  // 1. 幂等锁：防止并发创建同名 bucket
  locked, err := s.locker.Lock(ctx, "bucket:create:"+req.BucketName, 5*time.Second)
  if err != nil || !locked {
    return nil, common.ConflictErr.WithMsg("bucket is being created")
  }
  defer s.locker.Unlock(ctx, "bucket:create:"+req.BucketName)

  // 2. 业务校验
  exists, err := s.bucketRepo.ExistsByName(ctx, userCtx.UserID, req.BucketName)
  if err != nil {
    s.logger.Error("check bucket exists", zap.String("name", req.BucketName), zap.Error(err))
    return nil, common.DatabaseErr.WithErr(err)
  }
  if exists {
    return nil, common.ConflictErr.WithMsg("bucket already exists")
  }

  // 3. 持久化（Pattern C 事务：service 零 gorm 感知）
  var bucketDo *do.BucketDo
  txErr := s.txManager.RunInTx(ctx, func(tx adaptor.Tx) error {
    txBucket := s.bucketRepo.WithTx(tx)
    var e error
    bucketDo, e = txBucket.Create(ctx, &do.CreateBucket{
      UserID:     userCtx.UserID,
      BucketName: req.BucketName,
      Acl:        req.Acl,
    })
    return e
  })
  if txErr != nil {
    s.logger.Error("create bucket tx", zap.Error(txErr))
    return nil, toErrno(txErr)
  }

  s.logger.Info("bucket created", zap.String("name", req.BucketName), zap.Int64("user", userCtx.UserID))
  return toBucketResp(bucketDo), common.OK
}

// 多 repo 联动示例（PutObject：同时写 object 记录 + 更新 bucket 统计）
func (s *objectService) PutObject(ctx context.Context, userCtx *common.UserInfoCtx, req *dto.PutObjectReq, r io.Reader) (*dto.PutObjectResp, common.Errno) {
  // 1. 先写存储（不在事务内，避免持锁做 I/O）
  putResult, err := s.storage.Put(ctx, req.BucketName, req.ObjectKey, r)
  if err != nil {
    return nil, common.ServerErr.WithErr(err)
  }

  // 2. 事务：object 记录 + bucket 统计原子写入
  var objDo *do.ObjectDo
  txErr := s.txManager.RunInTx(ctx, func(tx adaptor.Tx) error {
    txObj    := s.objectRepo.WithTx(tx)
    txBucket := s.bucketRepo.WithTx(tx)

    var e error
    objDo, e = txObj.Create(ctx, &do.CreateObject{...})
    if e != nil { return e }

    return txBucket.UpdateStats(ctx, userCtx.UserID, req.BucketName, 1, putResult.Size)
  })
  if txErr != nil {
    // 存储已写入，记录孤儿对象日志，由后台 GC 清理
    s.logger.Error("put object tx failed, orphan storage",
      zap.String("path", putResult.StoragePath),
      zap.Error(txErr))
    return nil, toErrno(txErr)
  }
  return toObjectResp(objDo), common.OK
}
```

---

### 4️⃣ Repository 层 (`adaptor/repo/{module}/`)

```go
// adaptor/repo/bucket/ibucket.go
type IBucketRepo interface {
  // WithTx 返回绑定到 tx 的新 repo 实例；原有方法名不变，无任何 XxxWithTx 变体
  WithTx(tx adaptor.Tx) IBucketRepo

  Create(ctx context.Context, b *do.CreateBucket) (*do.BucketDo, error)
  GetByName(ctx context.Context, userID int64, name string) (*do.BucketDo, error)
  ExistsByName(ctx context.Context, userID int64, name string) (bool, error)
  ListByUser(ctx context.Context, userID int64) ([]*do.BucketDo, error)
  UpdateStats(ctx context.Context, userID int64, name string, objDelta, sizeDelta int64) error
  Delete(ctx context.Context, userID int64, name string) error
}

// consts/cache_keys.go  ← 全项目共用
const cacheKeyBucket = "oss:bucket:%d:%s"

func BucketCacheKey(userID int64, name string) string {
    return fmt.Sprintf(cacheKeyBucket, userID, name)
}

// adaptor/repo/bucket/bucket_repo.go
type bucketRepo struct {
  db  *gorm.DB
  rds redis.UniversalClient
}

func New(a adaptor.IAdaptor) IBucketRepo {
  return &bucketRepo{db: a.GetDB(), rds: a.GetRedis()}
}

// WithTx：唯一知道 Tx 底层是 *gorm.DB 的地方
func (r *bucketRepo) WithTx(tx adaptor.Tx) IBucketRepo {
  return &bucketRepo{
    db:  tx.(*gorm.DB), // 类型断言仅在 repo 实现层出现
    rds: r.rds,
  }
}

func (r *bucketRepo) Create(ctx context.Context, b *do.CreateBucket) (*do.BucketDo, error) {
  row := model.Bucket{UserID: b.UserID, BucketName: b.BucketName, Acl: b.Acl}
  if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
    return nil, wrapErr(err)
  }
  return r.toDo(&row), nil
}

func (r *bucketRepo) ExistsByName(ctx context.Context, userID int64, name string) (bool, error) {
  key := BucketCacheKey(userID, name)
  if exists, err := r.rds.Exists(ctx, key).Result(); err != nil {
    return false, err
  } else if exists == 1 {
    return true, nil
  }

  var count int64
  err := r.db.WithContext(ctx).Model(&model.Bucket{}).
    Where("user_id = ? AND bucket_name = ?", userID, name).
    Count(&count).Error
  if err != nil {
    return false, wrapErr(err)
  }
  return count > 0, nil
}

func (r *bucketRepo) ListByUser(ctx context.Context, userID int64) ([]*do.BucketDo, error) {
  var rows []model.Bucket
  if err := r.db.WithContext(ctx).
    Where("user_id = ?", userID).
    Order("created_at DESC").
    Find(&rows).Error; err != nil {
    return nil, wrapErr(err)
  }
  result := make([]*do.BucketDo, 0, len(rows)) // 预分配，避免扩容
  for i := range rows {
    result = append(result, r.toDo(&rows[i]))
  }
  return result, nil
}

// wrapErr：GORM 实现的错误映射，切库时只改这一个函数
func wrapErr(err error) error {
  if err == nil {
    return nil
  }
  if errors.Is(err, gorm.ErrRecordNotFound) {
    return repoerr.ErrNotFound
  }
  var mysqlErr *mysql.MySQLError
  if errors.As(err, &mysqlErr) {
    switch mysqlErr.Number {
    case 1062:       return repoerr.ErrDuplicate
    case 1451, 1452: return repoerr.ErrFKViolated
    }
  }
  return err
}
```

**Repo 规范**：
- 接口必须有 `WithTx(tx adaptor.Tx) IXxxRepo`，位于接口第一行
- **禁止** `CreateWithTx(tx *gorm.DB, ...)` 等变体——方法名在事务内外完全一致
- 类型断言 `tx.(*gorm.DB)` 只出现在 `WithTx` 实现中
- **所有 error 出口必须经过 `wrapErr`**
- 循环用 `for i := range rows` 避免复制大结构体

---

### 4½️ Repo 错误规范 (`adaptor/repo/repoerr/`)

```go
// adaptor/repo/repoerr/errors.go
package repoerr

import "errors"

var (
  // ErrNotFound 查询无结果（对应 gorm.ErrRecordNotFound / sql.ErrNoRows）
  ErrNotFound = errors.New("repo: record not found")

  // ErrDuplicate 唯一键冲突（对应 MySQL 1062 / PG 23505）
  ErrDuplicate = errors.New("repo: duplicate key")

  // ErrFKViolated 外键约束失败（对应 MySQL 1451/1452 / PG 23503）
  ErrFKViolated = errors.New("repo: foreign key violated")
)
```

**三层错误边界**（全局规则）：
```
Repo 实现层    →  wrapErr() 映射为 repoerr 哨兵，不允许 ORM 错误外泄
Service 层     →  toErrno() 映射为 common.Errno，不允许 repoerr 外泄到 Handler
Handler 层     →  统一序列化为 JSON 响应，不允许内部错误暴露给客户端
```

---

### 5️⃣ Storage 层 (`adaptor/storage/`)

```go
// adaptor/storage/istorage.go
type IStorage interface {
  Put(ctx context.Context, bucket, key string, r io.Reader) (*PutResult, error)
  Get(ctx context.Context, path string) (io.ReadCloser, int64, error)
  Delete(ctx context.Context, path string) error
  Exists(ctx context.Context, path string) (bool, error)
}

// adaptor/storage/local/local.go
type localStorage struct {
  basePath string
  bufPool  sync.Pool // 复用 copy buffer，避免频繁 GC
}

func New(basePath string) storage.IStorage {
  return &localStorage{
    basePath: basePath,
    bufPool: sync.Pool{
      New: func() any { return make([]byte, 32*1024) }, // 32KB copy buffer
    },
  }
}

func (s *localStorage) Put(ctx context.Context, bucket, key string, r io.Reader) (*storage.PutResult, error) {
  path := filepath.Join(s.basePath, bucket, key)
  if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
    return nil, fmt.Errorf("localStorage.Put mkdir: %w", err)
  }
  f, err := os.Create(path)
  if err != nil {
    return nil, fmt.Errorf("localStorage.Put create: %w", err)
  }
  defer f.Close()

  buf := s.bufPool.Get().([]byte)
  defer s.bufPool.Put(buf)

  h := md5.New()
  written, err := io.CopyBuffer(io.MultiWriter(f, h), r, buf) // 边写边算 MD5
  if err != nil {
    return nil, fmt.Errorf("localStorage.Put copy: %w", err)
  }
  return &storage.PutResult{
    Size:        written,
    Etag:        hex.EncodeToString(h.Sum(nil)),
    StoragePath: path,
  }, nil
}
```

---

### 6️⃣ Redis 层 (`adaptor/redis/`)

```go
// adaptor/redis/itoken.go
type IToken interface {
  CreateUploadToken(ctx context.Context, token string, req *dto.CreateUploadTokenReq, expire time.Duration) error
  CreateDownloadToken(ctx context.Context, token string, req *dto.CreateDownloadTokenReq, expire time.Duration) error
  GetUploadToken(ctx context.Context, token string) (*dto.CreateUploadTokenReq, error)
  GetDownloadToken(ctx context.Context, token string) (*dto.CreateDownloadTokenReq, error)
  GetUploadTokenFields(ctx context.Context, token string, fields ...string) (map[string]string, error)
  GetDownloadTokenFields(ctx context.Context, token string, fields ...string) (map[string]string, error)
  DeleteUploadToken(ctx context.Context, token string) error
  DeleteDownloadToken(ctx context.Context, token string) error
}

// adaptor/redis/imultipart.go
type IMultipart interface {
  SetTimeoutMultipartCancel(ctx context.Context, uploadID string, t time.Time) error
}

// adaptor/redis/ilocker.go
type ILock interface {
  AcquireLock(ctx context.Context, key string, uuid string, ttl time.Duration) (bool, error)
  ReleaseLock(ctx context.Context, key string, uuid string) error
  RefreshLock(ctx context.Context, key string, uuid string, ttl time.Duration) error
  CheckLock(ctx context.Context, key string, uuid string) (bool, error)
  ForceReleaseLock(ctx context.Context, key string) error
}

// adaptor/redis/ifilelock.go
type IFileLock interface {
  AcquireLock(ctx context.Context, bucketName string, objectName string, uuid string, ttl time.Duration) (bool, error)
  ReleaseLock(ctx context.Context, bucketName string, objectName string, uuid string) error
  RefreshLock(ctx context.Context, bucketName string, objectName string, uuid string, ttl time.Duration) (bool, error)
  CheckLock(ctx context.Context, bucketName string, objectName string, uuid string) (bool, error)
  ForceReleaseLock(ctx context.Context, bucketName string, objectName string) error
}

// adaptor/redis/ilifecycle.go
type ILifecycle interface {
  SetLifecycleEvent(ctx context.Context, bucketID int64, ruleID int64, prefix string, operation string, objectKey string, executeTime time.Time) error
  GetPendingLifecycleEvents(ctx context.Context, bucketID int64, ruleID int64, prefix string, operation string) ([]string, error)
  DelLifecycleEvent(ctx context.Context, bucketID int64, ruleID int64, prefix string, operation string, objectKey string) error
  ClearRuleEvents(ctx context.Context, bucketID int64, ruleID int64, prefix string) error
}
```

---

### 7️⃣ Adaptor 层 (`adaptor/`)

```go
// adaptor/tx.go
// Tx 是不透明类型——service 和接口定义层完全不知道底层是 gorm 还是别的 ORM
type Tx interface{}

// ITxManager 是 service 开启事务的唯一入口
type ITxManager interface {
  RunInTx(ctx context.Context, fn func(tx Tx) error) error
}

// adaptor/tx_manager.go
type gormTxManager struct{ db *gorm.DB }

func NewTxManager(db *gorm.DB) ITxManager {
  return &gormTxManager{db: db}
}

func (m *gormTxManager) RunInTx(ctx context.Context, fn func(Tx) error) error {
  return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
    return fn(tx)
  })
}

// adaptor/iadaptor.go
type IAdaptor interface {
  GetDB()         *gorm.DB           // 仅供 repo 构造时使用，service 层禁止调用
  GetRedis()      redis.UniversalClient
  GetTxManager()  ITxManager
  GetStorage()    storage.IStorage
  GetLocker()     redis.ILocker
  GetToken()      redis.IToken
  GetMultipart()  redis.IMultipart
  GetLogger()     *zap.Logger
}
```

---

## 🏗️ 数据模型分层

| 层级 | 位置 | 用途 | 特点 |
|------|------|------|------|
| **Model** | `adaptor/repo/model/*.gen.go` | GORM ORM 映射 | 自动生成，勿手改 |
| **DO** | `service/do/{module}.go` | Service 内部数据 | 完整字段，无序列化注解 |
| **DTO** | `service/dto/{module}.go` | HTTP 请求/响应 | JSON 注解，只含外部字段 |

---

## 📝 命名规范

| 元素 | 规范 | 示例 |
|------|------|------|
| **Handler 接口** | `I` + 模块 + `Handler` | `IBucketHandler`, `IObjectHandler` |
| **Service 接口** | `I` + 模块 + `Service` | `IBucketService`, `IObjectService` |
| **Repo 接口** | `I` + 模块 + `Repo` | `IBucketRepo`, `IObjectRepo` |
| **Infrastructure 接口** | `I` + 职责 | `IStorage`, `ILocker`, `IToken`, `ITxManager` |
| **实现 struct** | 小写（包私有） | `bucketService`, `bucketRepo`, `localStorage` |
| **构造函数** | `New(a IAdaptor)` | 返回接口类型，不返回 struct |
| **Repo 事务工厂** | `WithTx(tx Tx) IXxxRepo` | 接口第一个方法，必须声明 |
| **Repo 普通方法** | 动词 + 名词（无 Tx 后缀） | `Create`, `GetByName`, `UpdateStats`, `Delete` |
| **❌ 禁止** | `XxxWithTx(tx *gorm.DB, ...)` | 此模式已废弃，改用 `WithTx` 工厂 |
| **JSON 字段** | 小写下划线 | `bucket_name`, `upload_id` |

---

## 🔄 新增功能模块标准流程

以"审计日志"为例，**严格按顺序**：

```
1. 数据库 (init.sql)
   CREATE TABLE audit_logs (...);

2. 生成 ORM 模型
   cd adaptor/repo && gentool -c gen.yaml

3. 定义 DO/DTO
   service/do/audit.go    ← CreateAuditLog, AuditLogDo
   service/dto/audit.go   ← AuditLogResp

4. Repo 层（接口+实现）
   adaptor/repo/audit/iaudit.go         ← IAuditRepo interface
   adaptor/repo/audit/audit_repo.go     ← auditRepo struct，New() IAuditRepo

5. Service 层（接口+实现）
   service/audit/iservice.go   ← IAuditService interface
   service/audit/service.go    ← auditService struct，New(IAdaptor) IAuditService

6. Handler 层（接口+实现）
   api/auth/iaudit.go   ← IAuditHandler interface
   api/auth/audit.go    ← auditHandler struct，NewAuditHandler(IAuditService) IAuditHandler

7. Adaptor 扩展
   adaptor/iadaptor.go  ← 如需暴露新基础设施则添加方法

8. 注册路由
   router/router.go     ← RouterDeps 加入 AuditHandler IAuditHandler
```

---

## ✅ 高性能检查清单

代码生成前必须确认（逐项过，不可跳过）：

**接口设计**
- [ ] Handler/Service/Repo/Storage/Redis 每层都定义了接口？
- [ ] 构造函数返回接口类型（非 struct 指针）？
- [ ] 上层只 import 接口包，不 import 实现包？

**性能**
- [ ] 大文件用流式处理（`io.Reader`），未用 `io.ReadAll`？
- [ ] 批量结果用 `make(slice, 0, cap)` 预分配？
- [ ] 高频 copy 操作用 `sync.Pool` 复用 buffer？
- [ ] 并行独立操作用 `errgroup`？
- [ ] 热点路径避免了 map/slice 频繁扩容？

**错误处理**
- [ ] 所有 `error` 都被处理（无 `_ = err`）？
- [ ] 错误用 `fmt.Errorf("模块.方法: %w", err)` 包装？
- [ ] Service boundary 用 `errors.As` 转换为 `common.Errno`？
- [ ] 无 `panic`（除 main 初始化）？

**并发安全**
- [ ] 共享状态有锁保护或用 atomic？
- [ ] 没有在持锁状态下做 I/O？
- [ ] 所有 goroutine 有退出机制，无泄漏？

**事务（Pattern C）**
- [ ] Service 持有 `ITxManager`，不持有 `*gorm.DB`？
- [ ] 多步写操作通过 `txManager.RunInTx(ctx, func(tx Tx) error {...})` 包裹？
- [ ] 事务内通过 `repo.WithTx(tx)` 获取 tx 绑定实例？
- [ ] Repo 接口第一个方法是 `WithTx(tx adaptor.Tx) IXxxRepo`？
- [ ] 代码中无 `XxxWithTx(tx *gorm.DB, ...)` 变体方法？
- [ ] 类型断言 `tx.(*gorm.DB)` 只出现在 repo `WithTx` 实现中？
- [ ] 存储 I/O（`storage.Put`）在事务外执行？
- [ ] `defer cancel()` 配合 context？

**可观测性**
- [ ] 关键路径有 `zap.Logger` 日志（Error/Info 级别）？
- [ ] 日志字段用结构化 `zap.String`, `zap.Int64`, `zap.Error`？

---

## 📋 文件位置速查

| 层级 | 接口文件 | 实现文件 |
|------|----------|----------|
| Handler | `api/auth/i{module}.go` | `api/auth/{module}.go` |
| Service | `service/{module}/iservice.go` | `service/{module}/service.go` |
| Repo | `adaptor/repo/{module}/i{module}.go` | `adaptor/repo/{module}/{module}_repo.go` |
| **事务** | `adaptor/tx.go`（`Tx`, `ITxManager`） | `adaptor/tx_manager.go`（`gormTxManager`） |
| Storage | `adaptor/storage/istorage.go` | `adaptor/storage/{type}/{type}.go` |
| Locker | `adaptor/redis/ilocker.go` | `adaptor/redis/locker.go` |
| Token | `adaptor/redis/itoken.go` | `adaptor/redis/token.go` |
| Adaptor | `adaptor/iadaptor.go` | `adaptor/adaptor.go` |
| 路由 | `router/router.go` | — |
| 错误码 | `common/errno.go` | — |
| DO | `service/do/{module}.go` | — |
| DTO | `service/dto/{module}.go` | — |

---

## 📊 项目完成度（最新：2026-05-22）

### ✅ 已实现的核心模块（14+）

| 模块 | 状态 | 说明 |
|------|------|------|
| **Bucket 管理** | ✅ 完成 | 创建、列表、获取、更新、删除、ACL、版本控制 |
| **Object 存储** | ✅ 完成 | 上传、下载、元数据、删除、版本历史、删除标记 |
| **分片上传** | ✅ 完成 | 虚拟合并策略、24小时超时清理、并发控制 |
| **AK/SK 认证** | ✅ 完成 | Access Key/Secret Key、签名验证、临时 Token |
| **权限策略** | ✅ 完成 | Bucket Policy、Principal/Action/Resource/Condition 多维规则 |
| **生命周期管理** | ✅ 完成 | 存储类转移、自动过期删除、分片清理、默认规则 |
| **版本控制** | ✅ 完成 | Bucket 版本启用、object version_id 追踪、删除标记 |
| **分布式锁** | ✅ 完成 | Redis 原子操作、Lua 脚本、自动过期、Refresh 机制 |
| **事件规则** | ✅ 完成 | 事件类型管理、异步分发、前缀/后缀过滤、Webhook 回调 |
| **CORS 管理** | ✅ 完成 | 跨域资源共享规则、灵活配置 |
| **审计日志** | ✅ 完成 | 操作记录、日志查询、安全审计 |
| **流量统计** | ✅ 完成 | 日级指标收集、请求/流量统计、用户/Bucket 级粒度 |
| **预签名 Token** | ✅ 完成 | 上传/下载临时令牌、权限绑定、过期管理 |
| **视频处理** | ✅ 完成 | HLS 切片、AES-128 加密、播放令牌、转码任务队列 |

### 📚 文档覆盖度

| 文档 | 更新日期 | 覆盖范围 |
|------|----------|----------|
| **README.md** | 2026-05-22 | 项目概览、功能特性、快速开始、架构说明 |
| **PROJECT_INDEX.md** | 2026-05-22 | 详细模块索引、文件组织、数据流示例、认证流程、编译状态 |
| **MULTIPART_GUIDE.md** | 2026-05-22 | 分片上传详解、虚拟合并、数据库设计、性能考虑 |
| **POLICY_API.md** | 2026-05-22 | 权限策略 API、数据模型、支持的条件、配置示例 |
| **QUICK_REFERENCE.md** | 2026-05-22 | API 快速查询、常用命令、常见错误码 |
| **OBJECT_VERSIONING_DESIGN.md** | 已有 | 版本控制设计、删除标记、回滚语义 |
| **video.md** | 已有 | 视频处理计划、数据库设计、任务流程 |
| **task.md** | 已有 | 异步任务完整流程、状态管理、错误恢复 |

### 🏗️ 架构完整性

- ✅ **全层接口化**: Handler → Service → Repo → Storage/Redis
- ✅ **零 GORM 依赖**: Service 层通过 `ITxManager` 和 `Tx interface{}` 管理事务
- ✅ **错误三层边界**: Repo → Service → Handler（repoerr → Errno → JSON）
- ✅ **高性能设计**: 流式 I/O、缓冲池复用、并发池控制、连接复用
- ✅ **完整代码规范**: 命名规范、检查清单、分层说明、最佳实践文档

### 🎯 开发效率

| 工具 | 用途 | 状态 |
|------|------|------|
| GORM Gen | 自动生成 Model/Query | ✅ 配置完成 |
| Govertor | 类型安全对象转换 | ✅ 支持 |
| Docker Compose | 本地开发环境 | ✅ 配置完成 |
| Air | 热重载开发 | ✅ 支持 |
| MySQL 初始化 | 一键建表 | ✅ `init.sql` 完整 |

### 🚀 下一步方向（可选）

- [ ] 云存储适配（S3 兼容层、阿里云 OSS 等）
- [ ] 性能优化（缓存策略、CDN 集成、SQL 优化）
- [ ] 增强权限（Resource 标签、条件表达式增强）
- [ ] 更多编码格式（视频 H.265、音频 AAC 等）
- [ ] 监控告警（Prometheus 指标、告警规则）
- [ ] 备份恢复（数据备份、灾难恢复计划）

---

**项目状态**: 核心功能完整，架构规范清晰，文档体系完善，可投入生产使用。