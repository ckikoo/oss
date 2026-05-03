# OSS 项目快速参考 (2026-05-03)

## 🎯 关键改动总结

### ✅ 已完成：默认生命周期规则

**问题**: 之前创建bucket时，没有自动创建任何lifecycle规则，上传的对象永远保存在STANDARD存储

**解决方案**: 在 [service/bucket/service.go](service/bucket/service.go) CreateBucket 中添加自动规则创建

**具体改动**:
```go
// CreateBucket 中新增代码
defaultRules := []*do.CreateLifecycleRule{
    {
        BucketID: id,
        RuleName: "Default-IA-Transition",
        TransitionDays: 30,           // 30天转换到IA
        TransitionStorageClass: "IA",
        Status: 1,
    },
    {
        BucketID: id,
        RuleName: "Default-Archive-Transition", 
        TransitionDays: 90,           // 90天转换到ARCHIVE
        TransitionStorageClass: "ARCHIVE",
        Status: 1,
    },
    {
        BucketID: id,
        RuleName: "Default-Expiration",
        ExpirationDays: 180,          // 180天自动删除
        Status: 1,
    },
}

for _, rule := range defaultRules {
    srv.lifecycleRepo.CreateLifecycleRule(ctx, rule)
}
```

**效果**: 现在每创建一个bucket，自动生成3条生命周期规则

---

## 📊 项目模块速查

| 模块 | 位置 | 职能 | 状态 |
|------|------|------|------|
| **AccessKey** | `service/accesskey/` | 生成AK/SK、认证 | ✅ 完成 |
| **Bucket** | `service/bucket/` | 创建、管理bucket、自动规则 | ✅ 完成 |
| **Object** | `service/object/` | 上传、下载、删除对象 | ✅ 完成 |
| **Multipart** | `service/mutipart/` | 分片上传（虚拟合并） | ✅ 完成 |
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

---

**最后更新**: 2026-05-03  
**编译状态**: ✅ 通过  
**建议**: 下一步实现lifecycle规则执行器完成功能闭环
