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

**新增**: 统一的存储接口 `IStorage`，支持多种存储后端（本地、S3 等）

**核心模块**:
- [adaptor/storage/istorage.go](adaptor/storage/istorage.go) - 存储接口定义
- [adaptor/storage/local/local.go](adaptor/storage/local/local.go) - 本地磁盘实现
- [adaptor/adatpor.go](adaptor/adatpor.go) - Adaptor 整合存储

**Service 层改造**:
- `PutObject` 调用 `srv.storage.Put()` 替代直接文件操作
- `GetObject` 调用 `srv.storage.Get()` 替代 `os.Open()`
- `DeleteObject` 调用 `srv.storage.Delete()` 替代 `os.Remove()`
- `streamMultipartObject` 使用存储接口获取分片

**优势**:
- 业务逻辑与存储实现完全解耦
- 支持无缝切换存储后端（本地 → S3 → MinIO）
- 便于单元测试（可 mock 存储接口）

---

### ✅ DeleteObject 事务支持

**问题**: 删除对象涉及多个表更新（objects、buckets、users、metering），如中途失败可能导致数据不一致

**解决方案**: 使用 GORM 事务，所有数据库更新要么全部成功，要么全部回滚

**实现**:
- 新增事务方法: `GetByKeyWithTx`, `DeleteObjectWithTx`, `UpdateBucketStatsWithTx`, `UpdateStorageUsedWithTx`, `DeleteMultipartPartsWithTx`
- `service.DeleteObject()` 使用 `srv.db.WithContext(ctx).Transaction()` 包装所有数据库操作
- 事务外进行物理文件删除（通过存储接口），确保数据一致性

**效果**: DeleteObject 操作的数据完整性得到保证

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
| **Presigned** | `service/presigned/` | 预签名URL | ✅ 完成 |
| **Lifecycle** | `service/lifecycle/` | 规则管理 | ✅ 完成 |
| **执行器** | `timer/` | 后台执行规则 | ❌ 待做 |

---

## 🔍 按场景快速查找

### 我想要...

#### ...上传一个文件
→ [api/auth/object.go](api/auth/object.go) `PutObject()`  
→ [service/object/service.go](service/object/service.go) `PutObject()`

#### ...查询bucket的生命周期规则
→ [api/auth/lifecycle.go](api/auth/lifecycle.go) `ListLifecycleRules()`  
→ [service/lifecycle/service.go](service/lifecycle/service.go) `ListLifecycleRules()`

#### ...生成预签名URL
→ [api/auth/presigned.go](api/auth/presigned.go) `CreatePresignedUrl()`  
→ [service/presigned/service.go](service/presigned/service.go) `CreatePresignedUrl()`

#### ...了解认证流程
→ [api/auth/middleware.go](api/auth/middleware.go) `NewAccessKeyMiddleware()`
- 认证头格式为 `Authorization: OSS <access_key>:<timestamp>:<signature>`

#### ...添加新API端点
→ [api/auth/routes.go](api/auth/routes.go) 注册路由  
→ [api/auth/object.go](api/auth/object.go) 添加控制器  
→ [service/object/service.go](service/object/service.go) 添加业务逻辑

#### ...修改数据库表
→ 编辑 [init.sql](init.sql)  
→ 运行 `go run ./tools/gen.go` 自动生成模型

---

## 📋 生命周期规则执行 - 实现指南

### 当前状态
✅ 规则存储在数据库  
✅ 自动创建默认规则  
❌ **缺少执行逻辑**

### 需要实现的3个部分

#### 1️⃣ 规则扫描器 (Timer)
位置: `timer/lifecycle_scanner.go` (新建)

```go
// 定期扫描满足转移/删除条件的对象
func ScanAndProcessRules(ctx context.Context) {
    // 1. 从lifecycle_rules表查询已启用的规则
    // 2. 根据created_at vs rule.transition_days 判断是否应转移
    // 3. 将需要处理的对象ID写入Redis Stream
}
```

#### 2️⃣ 事件生产者 (Redis Publisher)
位置: `adaptor/redis/lifecycle.go` (完善)

```go
// 将待处理的任务写入Redis Stream
func PublishLifecycleEvent(uploadID, objectID, action string) {
    // 如: action = "transition-to-ia" / "transition-to-archive" / "delete"
    // 存储到 Redis Stream: oss:lifecycle:events
}
```

#### 3️⃣ 事件消费者 (Background Worker)
位置: `service/lifecycle/executor.go` (新建)

```go
// 从Redis Stream消费事件，执行转移/删除
func ConsumeLifecycleEvents(ctx context.Context) {
    // 1. 从Redis Stream读取事件
    // 2. 根据action执行操作:
    //    - transition-to-ia: 改变storage_class，可能涉及数据转移
    //    - transition-to-archive: 压缩存储或转移到冷存储
    //    - delete: 删除对象和物理文件
    // 3. 更新object.storage_class
    // 4. 删除消息
}
```

### 建议实现顺序
1. 在 `timer/lifecycle_scanner.go` 实现定期扫描 (使用Hertz的定时任务或独立goroutine)
2. 在 `adaptor/redis/lifecycle.go` 完善 PublishLifecycleEvent
3. 在 `service/lifecycle/executor.go` 实现消费者和执行逻辑
4. 在 `main.go` 中启动后台任务

### 性能考虑
- 每次扫描限制批量大小 (如1000个对象)
- 使用Redis Stream的消费者组(Consumer Group)支持并行消费
- 考虑添加重试机制处理失败的转移

---

## 🗂️ 文件树最新状态

```
oss/
├── ✅ PROJECT_INDEX.md              [新增] 完整索引文档
├── adaptor/
│   ├── repo/lifecycle/
│   │   ├── ✅ lifecycle_repo.go      规则CRUD
│   │   ├── ✅ ilifecycle.go          接口
│   │   └── (presigned类似)
│   └── redis/
│       └── ✅ lifecycle.go           【已修复】missing return
├── service/
│   ├── bucket/
│   │   └── ✅ service.go             【已增强】自动默认规则
│   ├── presigned/
│   │   └── ✅ service.go             预签名URL
│   └── lifecycle/
│       └── ✅ service.go             规则管理
├── api/auth/
│   ├── ✅ lifecycle.go               【新增】Lifecycle端点
│   ├── ✅ presigned.go               【新增】Presigned端点
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
X-Access-Key: {AK}
X-Secret-Key: {SK}
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
1. 检查X-Access-Key和X-Secret-Key是否正确
2. 在 [api/auth/middleware.go](api/auth/middleware.go) 添加日志
3. 验证数据库中access_keys表的secret_key_hash值

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
| 添加路由 | `api/auth/routes.go` |