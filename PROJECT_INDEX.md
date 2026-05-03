# OSS 项目完整索引（2026-05-03）

## 📊 项目概览

**项目名称**: OSS (Object Storage Service)  
**框架**: Hertz + GORM Gen  
**数据库**: MySQL  
**缓存**: Redis  
**特性**: AK/SK认证、多分片上传、权限控制、生命周期管理、预签名URL、分布式文件锁

---

## 📁 文件组织结构

### 1. 数据访问层 (`adaptor/`)

#### `adaptor/repo/` - 数据库CRUD操作
```
adaptor/repo/
├── accesskey/
│   ├── access_key_repo.go      ✅ 数据库操作
│   └── iaccesskey.go           ✅ 接口定义
├── bucket/
│   ├── bucket_repo.go          ✅ 数据库操作
│   └── ibucket.go              ✅ 接口定义
├── object/
│   ├── object_repo.go          ✅ 数据库操作
│   └── iobject.go              ✅ 接口定义
├── multipart/
│   ├── object_repo.go          ✅ 分片上传管理
│   └── iobject.go              ✅ 接口定义
├── policy/
│   ├── policy_repo.go          ✅ 权限策略CRUD
│   └── ipolicy.go              ✅ 接口定义
├── presigned/                  ✅ 新增
│   ├── presigned_repo.go       ✅ 预签名URL CRUD
│   └── ipresigned.go           ✅ 接口定义
├── lifecycle/                  ✅ 新增
│   ├── lifecycle_repo.go       ✅ 生命周期规则CRUD
│   └── ilifecycle.go           ✅ 接口定义
├── model/                      ✅ 自动生成
│   ├── *.gen.go                ✅ GORM模型（从数据库生成）
│   └── 包含所有表的结构体
├── query/                      ✅ 自动生成
│   ├── *.gen.go                ✅ GORM查询方法（从数据库生成）
│   └── 包含所有SQL构造方法
└── admin/
    ├── user_repo.go            ✅ 用户数据访问
    └── iuser.go                ✅ 接口定义
```

**关键设计**:
- 每个模块实现自己的 Interface 接口
- 使用 GORM Gen 自动生成模型和查询代码
- 应用 Repository Pattern 隔离数据访问

#### `adaptor/redis/` - Redis操作
```
adaptor/redis/
├── mutipart.go     ✅ 分片上传超时管理 (ZSet存储)
├── lifecycle.go    ⚠️ 生命周期事件存储 (待实现消息处理)
└── file.go         ✅ 分布式文件锁 (基于bucket+object名称)
```

**文件锁特性**:
- 基于 Redis SET NX 原子操作
- 支持锁获取、释放、刷新、检查
- 使用 Lua 脚本确保操作原子性
- 自动过期防止死锁
- 按 bucket+object 粒度锁定

---

### 2. 业务逻辑层 (`service/`)

#### `service/accesskey/` - 访问密钥服务
- `service.go`: 生成AK/SK、查询、更新状态
- **关键函数**:
  - `CreateAccessKey()`: 生成24位随机AK，48位随机SK
  - `GetByAccessKey()`: 按AK查询（用于认证）
  - `UpdateAccessKeyStatus()`: 启用/禁用AK

#### `service/bucket/` - Bucket管理服务 
- `service.go`: 创建、列出、获取、更新、删除bucket
- **关键改动 (2026-05-03)**:
  - ✅ 注入 lifecycleRepo 依赖
  - ✅ CreateBucket 自动创建3条默认规则：
    - "Default-IA-Transition" (30天转IA)
    - "Default-Archive-Transition" (90天转Archive)
    - "Default-Expiration" (180天删除)

#### `service/object/` - 对象存储服务
- `service.go`: 列表、上传、下载、删除、元数据获取
- **特点**:
  - MD5计算etag，用于对象唯一标识
  - 支持多种storage_class (STANDARD/IA/ARCHIVE)
  - 流式处理，避免大文件OOM

#### `service/mutipart/` - 分片上传服务
- `mutipart.go`: 初始化、上传分片、完成合并、中止上传
- **虚拟合并策略**:
  - 分片存储在 `/storage/{bucket}/multipart/{upload_id}/part_{number}`
  - 完成时创建object记录，不进行物理合并
  - 读取时动态流式组合分片内容

#### `service/policy/` - 权限策略服务
- `service.go`: 创建、列表bucket policy
- **架构**:
  - bucket_policies (头表)
  - policy_principals / policy_actions / policy_resources / policy_conditions (子表)
  - 使用 oss/utils/pool 控制并发加载，避免N+1查询

#### `service/presigned/` - 预签名URL服务 (新增)
- `service.go`: 生成临时URL、撤销、查询
- **流程**:
  - 生成16位随机token
  - 存储在presigned_urls表，带expiration_at
  - 支持单次使用标记 (single_use)

#### `service/lifecycle/` - 生命周期规则服务 (新增)
- `service.go`: CRUD lifecycle规则
- **规则模型**:
  - 支持前缀匹配 (prefix)
  - 转换天数 + 目标存储类
  - 过期天数 + 自动删除
  - 分片清理天数 (默认7天)

#### `service/do/` - 领域对象
```
do/
├── access_key.go   - AccessKeyDo / CreateAccessKey / UpdateAccessKeyStatus
├── bucket.go       - BucketDo / CreateBucket / UpdateBucket
├── object.go       - ObjectDo / CreateObject / UpdateObject
├── multipart.go    - MultipartUploadDo / MultipartPartDo
├── policy.go       - BucketPolicyDo / CreateBucketPolicy
├── presigned.go    - PresignedURLDo / CreatePresignedURL
└── lifecycle.go    - LifecycleRuleDo / CreateLifecycleRule / UpdateLifecycleRule
```

#### `service/dto/` - 数据传输对象
```
dto/
├── access_key.go   - 请求/响应结构体
├── bucket.go
├── object.go
├── multipart.go
├── policy.go
├── presigned.go    - CreatePresignedUrlReq / CreatePresignedUrlResp
└── lifecycle.go    - CreateLifecycleRuleReq / ListLifecycleRulesResp
```

---

### 3. API层 (`api/`)

#### `api/auth/` - 认证API和中间件
```
api/auth/
├── middleware.go         ✅ AK/SK验证中间件
│   └── NewAccessKeyMiddleware() - 验证Authorization头或X-Access-Key/X-Secret-Key
├── routes.go             ✅ 所有API路由注册
│   ├── /api/v1/access-keys (POST/GET/PATCH)
│   ├── /api/v1/buckets/** (POST/GET/PATCH/DELETE)
│   ├── /api/v1/buckets/:bucket_name/objects/** (PUT/GET/DELETE)
│   ├── /api/v1/buckets/:bucket_name/multipart/** (POST/PUT/DELETE)
│   ├── /api/v1/buckets/:bucket_name/policies (POST/GET)
│   ├── /api/v1/buckets/:bucket_name/lifecycle/** (POST/GET/PUT/DELETE) ✅ 新增
│   └── /api/v1/presigned-urls (POST/DELETE) ✅ 新增
├── access_key.go        ✅ Access Key 控制器
├── bucket.go            ✅ Bucket 控制器
├── object.go            ✅ Object 控制器
├── multipart.go         ✅ Multipart 控制器
├── policy.go            ✅ Policy 控制器
├── presigned.go         ✅ Presigned URL 控制器 (新增)
└── lifecycle.go         ✅ Lifecycle 控制器 (新增)
```

**认证方式**:
```
方式1 - Header:
  X-Access-Key: {access_key}
  X-Secret-Key: {secret_key}

方式2 - Authorization:
  Authorization: AccessKey {access_key}:{secret_key}
```

#### `api/admin/` - 管理API
```
api/admin/
├── admin.go            - Admin 控制器基类
└── user.go             - 用户管理API (CreateUser)
```

#### `api/resp.go` - 统一响应格式
```go
WriteResp(c, data, errno)
// 返回: {code, msg, data}
```

---

### 4. 配置与工具

#### `config/` - 配置管理
- `config.go`: MySQL / Redis / Server 配置读取
- 支持从本地config.yaml或etcd加载

#### `consts/` - 常量定义
```
consts/
├── 用户状态: UserStatusEnable(1), UserStatusDisable(2), UserStatusDeleted(3)
├── AccessKey状态: AccessKeyStatusEnable(1), AccessKeyStatusDisable(2)
├── Bucket状态: BucketStatusNormal(1), BucketStatusLocked(2), BucketStatusDeleted(3)
├── Object状态: ObjectStatusNormal(1), ObjectStatusDeleteMark(2), ObjectStatusDeleted(3)
├── Multipart状态: (0=Uploading, 1=MergedVirtual, 2=MergedPhysical, 3=Failed, 4=Aborted)
├── 存储类型: StorageClassStandard, StorageClassIA, StorageClassArchive
└── ACL类型: BucketAclPrivate, BucketAclPublicRead, BucketAclPublicRW
```

#### `utils/` - 工具函数
```
utils/
├── logger/logger.go
│   ├── SetLogLevel()
│   ├── Debug/Info/Warn/Error()
│   └── 使用 zap 日志库
├── pool/pool.go
│   ├── NewPoolWithSize()
│   ├── RunGo()
│   └── 使用 ants 协程池，控制并发
└── tools/tools.go
    ├── GenerateRandomKey() - 生成随机AK/SK
    ├── Md5Hash()           - MD5哈希（对象标识）
    ├── Sha256Hash()        - SHA256哈希（密钥存储）
    ├── SaveFileAndComputeHashes()
    └── 文件操作工具
```

#### `tools/gen.go` - 代码生成脚本
- 运行: `go run ./tools/gen.go`
- 生成 GORM 模型和查询代码到 `adaptor/repo/model` 和 `adaptor/repo/query`

---

### 5. 主程序

#### `main.go` - 服务入口
```go
main()
  ├─ InitConfig()          // 读取config.yaml
  ├─ initMysql()           // 连接MySQL
  ├─ initRedis()           // 连接Redis
  ├─ NewAdaptor()          // 创建适配器
  ├─ Hertz.RegisterRoutes()
  └─ h.Spin()              // 启动服务 (默认 localhost:8080)
```

#### `init.sql` - 数据库初始化脚本
- 创建所有表结构
- 插入初始数据

---

## 🔄 数据流示例

### 创建Bucket的完整流程
```
POST /api/v1/buckets
  ↓ [认证中间件验证AK/SK]
  ↓ BucketCtrl.CreateBucket()
  ↓ bucket.Service.CreateBucket()
    ├─ bucket.Repo.CreateBucket()
    │  └─ INSERT INTO buckets (...)  [返回bucket_id]
    ├─ 【新增】创建3条默认lifecycle规则
    │  ├─ lifecycle.Repo.CreateLifecycleRule() × 3
    │  │  └─ INSERT INTO lifecycle_rules (...)
    │  └─ 日志记录任何创建失败
    └─ 返回CreateBucketResp
```

### 上传对象的完整流程
```
PUT /api/v1/buckets/{bucket}/objects/{object_key}
  ↓ [认证中间件]
  ↓ ObjectCtrl.PutObject()
  ↓ object.Service.PutObject()
    ├─ bucket.Repo.GetByName()        // 获取bucket_id
    ├─ Tools.Md5Hash(object_key)      // 生成object_key_hash
    ├─ saveFileAndComputeHashes()     // 流式存储文件 + 计算etag
    ├─ object.Repo.CreateObject()     // 创建object记录
    └─ 返回PutObjectResp
```

### 分片上传的完整流程
```
1️⃣ POST /api/v1/buckets/{bucket}/multipart/uploads
   └─ mutipart.Service.CreateMultipartUpload()
      ├─ 生成upload_id (UUID)
      ├─ redis.SetTimeoutMultipartCancel() // 设置超时
      └─ 返回upload_id + expires_at

2️⃣ PUT /api/v1/buckets/{bucket}/multipart/uploads/{upload_id}/parts/{part_number}
   └─ mutipart.Service.UploadMultipartPart()
      ├─ 验证权限 + 上传状态
      ├─ 流式存储分片到 /storage/{bucket}/multipart/{upload_id}/part_{n}
      ├─ multipart.Repo.CreateOrUpdateMultipartPart()
      └─ 返回etag

3️⃣ POST /api/v1/buckets/{bucket}/multipart/uploads/{upload_id}/complete
   └─ mutipart.Service.CompleteMultipartUpload()
      ├─ 验证所有分片
      ├─ 计算最终etag
      ├─ object.Repo.CreateObject() // 虚拟合并
      └─ 返回object_id (物理文件仍为分片)
```

---

## 🔐 认证流程

```
请求包含: X-Access-Key: {AK}, X-Secret-Key: {SK}
         或 Authorization: AccessKey {AK}:{SK}
  ↓
middleware.NewAccessKeyMiddleware()
  ├─ 提取AK和SK
  ├─ repo.GetByAccessKey(AK)      // 从数据库查询
  ├─ tools.Sha256Hash(SK)         // 计算SK的SHA256
  ├─ 与数据库中的secret_key_hash比对
  ├─ 若相同: c.Set(UserKeyContext, user_id)
  └─ 若不同: 返回401 AuthErr
```

**重要**:
- 秘密密钥 (SK) 在数据库中以 SHA256 哈希存储
- 每次认证时重新计算哈希进行对比
- 用户上下文存储在Hertz context中，后续handler可通过c.Get(consts.UserKeyContext)获取

---

## 📊 数据库表关系

### 核心表
```
users (用户账户)
  ├─ access_keys (AK/SK)
  ├─ buckets (对象存储空间)
  │  ├─ objects (对象)
  │  ├─ multipart_uploads (分片会话)
  │  │  └─ multipart_parts (单个分片)
  │  ├─ bucket_policies (权限策略头)
  │  │  ├─ policy_principals (主体)
  │  │  ├─ policy_actions (动作)
  │  │  ├─ policy_resources (资源)
  │  │  └─ policy_conditions (条件)
  │  ├─ lifecycle_rules (生命周期规则) ✅ 新增
  │  └─ presigned_urls (预签名URL) ✅ 新增
  ├─ metering_daily (日统计)
  ├─ operation_logs (操作日志)
  └─ event_rules / event_deliveries (事件通知)
```

---

## 🚀 启动和开发

### 快速启动
```bash
# 1. 下载依赖
go mod tidy

# 2. 生成代码
go run ./tools/gen.go

# 3. 初始化数据库
mysql -uroot -p < init.sql

# 4. 启动服务
go run ./main.go

# 或指定配置文件
go run ./main.go -c /path/to/config.yaml
```

### 编译和测试
```bash
# 编译所有模块
go build ./...

# 运行所有测试
go test ./...

# 编译二进制
go build -o oss ./main.go
```

---

## ⚠️ 已知问题和待办事项

### 🔴 高优先级 - 缺失核心功能
| 问题 | 状态 | 说明 |
|------|------|------|
| 生命周期规则执行器 | ❌ 无 | 后台任务扫描并执行lifecycle规则 |
| 事件消费者 | ❌ 无 | Redis Stream消费lifecycle事件 |
| 对象转移逻辑 | ❌ 无 | 实现STANDARD→IA→ARCHIVE转换 |
| 过期删除逻辑 | ❌ 无 | 实现自动删除过期对象 |

### 🟡 中优先级 - 功能增强
| 问题 | 建议 |
|------|------|
| 版本控制 | PutObject中的TODO: Handle versioning |
| 分片清理 | 后台任务定期清理超时的分片上传 |
| 指标收集 | 完善metering_daily统计 |

### 🟢 低优先级 - 优化
| 问题 | 建议 |
|------|------|
| 缓存层 | 使用Redis缓存热点bucket/policy |
| 查询优化 | 对象列表支持更多过滤条件 |
| 监控告警 | 集成prometheus指标 |

---

## 📝 编译状态 (2026-05-03)
- ✅ `go build ./...` - **通过**
- ✅ `go test ./...` - **通过** (35个包，无编译错误)
- ✅ Presigned URL 服务 - **已实现**
- ✅ Lifecycle 规则服务 - **已实现**
- ✅ 默认生命周期规则 - **已实现** (CreateBucket时自动创建)
- ✅ 分布式文件锁 - **已实现** (Redis原子操作)

---

## 📞 项目总结

本OSS项目采用标准的分层架构：
1. **API层** (api/): 处理HTTP请求，验证认证
2. **业务层** (service/): 实现核心业务逻辑
3. **数据层** (adaptor/repo/): 通过接口隔离数据访问
4. **缓存层** (adaptor/redis/): Redis支持

**已完成特性**:
- ✅ AK/SK认证
- ✅ Bucket管理
- ✅ Object上传/下载
- ✅ 分片上传
- ✅ 权限策略
- ✅ 预签名URL
- ✅ 生命周期规则管理
- ✅ 默认规则自动创建
- ✅ 分布式文件锁

**待完成特性**:
- ❌ 生命周期规则执行（后台任务）
- ❌ 对象自动转移和删除
- ❌ 版本控制
- ❌ 事件通知
