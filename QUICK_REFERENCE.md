# OSS 项目快速参考 (2026-05-06)

## 🎯 最新改动总结

### ✅ ACL 控制完善

**新增**: Bucket 和 Object 级别的访问控制列表 (ACL)

**核心模块**:
- [router/acl.go](router/acl.go) - ACL 中间件实现
- [api/auth/object.go](api/auth/object.go) - Object ACL 解析
- [consts/consts.go](consts/consts.go) - ACL 常量定义

**ACL 级别**:
- **Bucket ACL**: Private (仅所有者), Public-Read (所有人可读), Public-RW (所有人可读写)
- **Object ACL**: Inherit (继承 Bucket), Private (仅所有者), Public-Read (所有人可读)

**中间件检查**:
- `NewBucketACLMiddleware`: 检查 Bucket 操作权限 (创建、更新、删除等)
- `NewObjectACLMiddleware`: 检查 Object 操作权限，支持 Object 特定 ACL

**优势**:
- 细粒度权限控制
- 支持 Bucket 和 Object 级别 ACL
- 自动继承和覆盖机制
- 防止未授权访问

---

### ✅ 存储层架构：Adaptor 与 Service 集成

**新增**: 统一的存储接口 `IStorage`，支持多种存储后端（本地、S3 等），并支持 `context.Context` 传递。

**核心模块**:
- [adaptor/storage/istorage.go](adaptor/storage/istorage.go) - 存储接口定义
- [adaptor/storage/local/local.go](adaptor/storage/local/local.go) - 本地磁盘实现
- [adaptor/adatpor.go](adaptor/adatpor.go) - Adaptor 整合存储

**Service 层改造**:
- `PutObject` 调用 `srv.storage.Put(ctx, ...)` 替代直接文件操作
- `GetObject` 调用 `srv.storage.Get(ctx, ...)` 替代 `os.Open()`
- `DeleteObject` 调用 `srv.storage.Delete(ctx, ...)` 替代 `os.Remove()`
- `streamMultipartObject` 使用存储接口获取分片
- `UploadMultipartPart` / `DeletePart` / `DeleteParts` 也使用上下文传递

**优势**:
- 业务逻辑与存储实现完全解耦
- 支持无缝切换存储后端（本地 → S3 → MinIO）
- 便于单元测试（可 mock 存储接口）

---

### ✅ 基础设施改进

**新增**: 错误处理增强、并发控制优化、接口化设计

**核心模块**:
- [utils/pool/pool.go](utils/pool/pool.go) - 协程池错误返回
- [timer/timer.go](timer/timer.go) - 独立定时器间隔
- [adaptor/repo/metering/metering_repo.go](adaptor/repo/metering/metering_repo.go) - 接口化设计
- [service/event/service.go](service/event/service.go) - 返回值一致性

**改进详情**:
- **Pool 错误处理**: `RunGo` 方法现在返回错误，防止静默丢弃任务
- **Timer 优化**: 任务、生命周期、事件使用独立定时器（30s、1min、10s），避免饥饿
- **接口化**: MeteringRepo 改为接口类型，提高可测试性和解耦
- **返回值统一**: Service 层成功返回统一使用 `common.OK`，失败使用 `common.Errno{}`

**优势**:
- 更好的错误可见性
- 防止任务执行饥饿
- 提高代码可维护性
- 统一错误处理语义

---

## 📊 项目模块速查

| 模块 | 位置 | 职能 | 状态 |
|------|------|------|------|
| **Storage** | `adaptor/storage/` | 统一存储接口 | ✅ 完成 |
| **AccessKey** | `service/accesskey/` | 生成AK/SK、认证 | ✅ 完成 |
| **Bucket** | `service/bucket/` | 创建、管理bucket、自动规则 | ✅ 完成 |
| **Object** | `service/object/` | 上传、下载、删除对象 | ✅ 完成 |
| **Multipart** | `service/multipart/` | 分片上传（虚拟合并） | ✅ 完成 |
| **Policy** | `service/policy/` | 权限策略管理 | ✅ 完成 |
| **Lifecycle** | `service/lifecycle/` | 规则管理 | ✅ 完成 |
| **执行器** | `timer/` | 后台任务执行器（异步 multipart 合并、超时清理） | ✅ 完成 |

---

## 🔍 按场景快速查找

### 我想要...

#### ...上传一个文件
→ [api/auth/object.go](api/auth/object.go) `PutObject()`  
→ [service/object/service.go](service/object/service.go) `PutObject()`

#### ...查询bucket的生命周期规则
→ [api/auth/lifecycle.go](api/auth/lifecycle.go) `ListLifecycleRules()`  
→ [service/lifecycle/service.go](service/lifecycle/service.go) `ListLifecycleRules()`

#### ...了解认证流程
→ [router/auth.go](router/auth.go) `NewAccessKeyMiddleware()`
- 认证头格式为 `Authorization: OSS <access_key>:<timestamp>:<signature>`
- GET object 下载还支持 `?token={token}` 查询参数

#### ...添加新API端点
→ [router/auth.go](router/auth.go) `NewAccessKeyMiddleware()`
- 认证头格式为 `Authorization: OSS <access_key>:<timestamp>:<signature>`
- GET object 下载还支持 `?token={token}` 查询参数

#### ...添加新API端点
→ [router/router.go](router/router.go) 注册路由  
→ [api/auth/object.go](api/auth/object.go) 添加控制器  
→ [service/object/service.go](service/object/service.go) 添加业务逻辑

#### ...修改数据库表
→ 编辑 [init.sql](init.sql)  
→ 运行 `go run ./tools/gen.go` 自动生成模型

---

## 📋 生命周期规则执行 - 当前状态

### 已实现
- ✅ 规则存储在数据库
- ✅ 创建 Bucket 时自动生成默认生命周期规则
- ✅ `timer/timer.go` 已实现后台任务框架，支持 Redis 任务队列消费
- ✅ 当前后台任务已支持 `PHYSICAL_MERGE` 和 `ABORT_MULTIPART` 两类 multipart 任务
- ✅ `timer/scan_lifecycle.go` 实现了生命周期规则扫描器（按批扫描对象，生成事件）
- ✅ `timer/lifecycle.go` 实现了生命周期事件消费和执行器
- ✅ 对象存储类转移（Transition）功能
- ✅ 对象过期删除（Expiration）功能

### 工作流程
1. **扫描阶段** (`handlerScanTableLifecycleEvents`)：
   - 定时扫描所有活跃的生命周期规则
   - 对每条规则，批量查询符合条件的对象
   - 将待处理事件写入 Redis ZSet（按执行时间排序）

2. **执行阶段** (`handlerLifecycleEvents`)：
   - 从 Redis 读取待执行的生命周期事件
   - 对于 Transition 事件：更新对象的存储类
   - 对于 Expiration 事件：获取分布式锁后删除对象

### 待完成
- ❌ 版本控制支持
- ❌ 事件通知机制

### 说明
- `timer/timer.go` 负责启动以下后台任务（独立定时器）：
  - `handlerTask` (30s 间隔) - 消费 multipart 合并和超时清理任务
  - `handlerLifecycleEvents` (1min 间隔) - 执行生命周期事件
  - `handlerScanTableLifecycleEvents` (1min 间隔) - 扫描生命周期规则并生成事件
  - `handlerEventDeliveries` (10s 间隔) - 处理事件通知
- Lifecycle 规则管理和执行均已完成

### 性能考虑
- 扫描任务限制批量大小（100条每批）
- 事件执行采用协程池并发处理（大小为 CPU * 2）
- 对象删除使用分布式锁防止并发删除
- 事件执行应支持并行消费与重试
- 对象迁移/删除任务需保持幂等性和失败回滚能力

---

## 🗂️ 文件树最新状态

```
oss/
├── ✅ PROJECT_INDEX.md              [新增] 完整索引文档
├── adaptor/
│   ├── repo/lifecycle/
│   │   ├── ✅ lifecycle_repo.go      规则CRUD
│   │   ├── ✅ ilifecycle.go          接口
│   └── redis/
│       └── ✅ lifecycle.go           【已修复】missing return
├── service/
│   ├── bucket/
│   │   └── ✅ service.go             【已增强】自动默认规则
│   └── lifecycle/
│       └── ✅ service.go             规则管理
├── api/auth/
│   ├── ✅ lifecycle.go               【新增】Lifecycle端点
│   └── ✅ routes.go                  【已更新】路由注册
└── main.go
    ├── ✅ 编译通过
    ├── ✅ 测试通过
    └── ✅ 准备就绪
```

---

## 🧪 测试清单

```bash
# 1. 编译检查
go build ./...
✅ 通过

# 2. 测试运行
go test ./...
✅ 通过 (35个包)

# 3. 创建bucket测试
POST http://localhost:8080/api/v1/buckets
Authorization: OSS <access_key>:<timestamp>:<signature>
{
  "user_id": 1,
  "name": "test-bucket-2",
  "region": "cn-hz"
}
✅ 应该自动创建3条lifecycle规则

# 4. 查询规则
GET http://localhost:8080/api/v1/buckets/test-bucket-2/lifecycle
✅ 应该返回3条默认规则
```

---

## 📌 重要常数和枚举

### 存储类型
```go
consts.StorageClassStandard = "STANDARD"  // 标准存储
consts.StorageClassIA       = "IA"        // 低频存储 (Infrequent Access)
consts.StorageClassArchive  = "ARCHIVE"   // 冷存档 (Long-term Archive)
```

### 对象状态
```go
consts.ObjectStatusNormal     = 1  // 正常
consts.ObjectStatusDeleteMark = 2  // 删除标记(版本控制)
consts.ObjectStatusDeleted    = 3  // 已删除
```

### Bucket状态
```go
consts.BucketStatusNormal  = 1  // 正常
consts.BucketStatusLocked  = 2  // 锁定
consts.BucketStatusDeleted = 3  // 已删除
```

---

## 🔧 常见开发任务

### 添加新的生命周期规则类型
1. 修改 [init.sql](init.sql) 中lifecycle_rules表
2. 运行 `go run ./tools/gen.go` 更新模型
3. 在 [service/lifecycle/service.go](service/lifecycle/service.go) 添加验证逻辑
4. 在 [api/auth/lifecycle.go](api/auth/lifecycle.go) 的API中使用

### 修改默认规则配置
1. 编辑 [service/bucket/service.go](service/bucket/service.go) CreateBucket方法
2. 修改 defaultRules 数组中的参数
3. 重新编译

### 调试认证问题
1. 检查 `Authorization: OSS <access_key>:<timestamp>:<signature>` 是否正确
2. 在 [router/auth.go](router/auth.go) 添加日志
3. 验证数据库中 access_keys 表的 secret_key_hash 值

---

## 📞 快速参考

| 需求 | 命令/文件 |
|------|---------|
| 启动服务 | `go run ./main.go` |
| 编译 | `go build ./...` |
| 生成模型 | `go run ./tools/gen.go` |
| 初始化DB | `mysql -uroot -p < init.sql` |
| 查看日志 | `utils/logger/logger.go` 配置 |
| 修改常数 | `consts/consts.go` |
| 添加路由 | `router/router.go` |