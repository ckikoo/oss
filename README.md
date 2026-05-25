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

### 核心功能
- **AK/SK 认证**: 支持 Access Key 和 Secret Key 认证，保护所有 API 访问
- **Bucket 管理**: 创建、列出、获取、更新、删除 bucket，支持 ACL 和版本控制配置
- **Object 存储**: 上传、下载、获取元数据、删除对象，支持多存储类型
- **Multipart Upload**: 支持分片上传，虚拟合并策略，高效处理大文件

### 高级功能
- **版本控制**: 支持对象版本管理、删除标记、永久删除和版本回滚（详见 [OBJECT_VERSIONING_DESIGN.md](OBJECT_VERSIONING_DESIGN.md)）
- **权限控制**: 基于 JSON 的细粒度权限系统，支持 Bucket Policy 和多维策略规则
  - 使用 `utils/pool` 控制并发加载，避免 N+1 查询（详见 [POLICY_API.md](POLICY_API.md)）
- **生命周期管理**: 支持对象存储类转换、过期删除、分片清理规则
  - Bucket 创建时自动生成默认规则（30天转IA、90天转Archive、180天删除）
- **存储类型**: STANDARD、IA（低频访问）、ARCHIVE（归档）三种存储类

### 企业功能  
- **视频处理**: 视频转码、HLS 切片、AES-128 播放加密、播放授权（详见 [video.md](doc/video.md)）
- **事件规则**: 支持 Bucket 事件通知（PUT、DELETE、POST 等），异步任务队列
- **CORS 规则**: 灵活的跨域资源共享配置
- **审计日志**: 完整的操作审计，支持数据查询和分析
- **流量统计**: 日级流量与请求类型统计，支持用户和 Bucket 级粒度
- **分布式锁**: 基于 Redis 的文件锁机制，支持原子操作和死锁预防

### 安全 & 性能
- **临时 Token**: 支持上传/下载预签名 URL，避免 AK/SK 泄露
- **流式处理**: 大文件上传/下载流式处理，避免内存溢出
- **并发控制**: Redis 分布式锁确保并发安全

## 缓存策略（Cache Strategy）

- 对于**稳定的只读元数据**（例如 `bucket`、`object` 元数据、`video` 的 transcode/profile 信息），系统采用本地 LRU + Redis 的分层缓存，并通过发布/订阅机制在实例间广播失效，详见 [doc/VIDEO_CACHE_DESIGN.md](doc/VIDEO_CACHE_DESIGN.md)。
- 对于**高写、易变的数据**（例如 multipart uploads / multipart parts），不使用跨实例缓存，直接以数据库为单一信任源；相关考量见 `doc/MULTIPART_GUIDE.md` 的“缓存考虑”节。


> **快速链接**：[项目索引](doc/PROJECT_INDEX.md) | [对象版本设计](doc/OBJECT_VERSIONING_DESIGN.md) | [多部分上传](doc/MULTIPART_GUIDE.md) | [权限 API](doc/POLICY_API.md) | [视频处理](doc/video.md)


## 分布式文件锁

基于 Redis 的分布式锁机制，确保对同一文件对象的并发操作安全。

**特性**:
- ✅ **原子性**: Redis SET NX 命令确保锁唯一性
- ✅ **安全性**: Lua 脚本确保只有锁持有者才能释放
- ✅ **自动过期**: 防止死锁，支持 TTL 设置
- ✅ **可续期**: 支持锁续期和状态检查
- ✅ **高性能**: 基于内存的 Redis 操作

**使用场景**: 分片上传并发控制、对象删除保护、并发更新防护

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

复制示例配置后再填写本地连接信息和密钥：

```bash
cp config.example.yaml config.yaml
# 编辑 config.yaml，设置 MySQL、Redis 和 security.aes_key
```

配置加载优先级：

1. 启动参数 `-e` 指定 etcd 地址时，优先从 etcd 的配置键加载。
2. 未启用 etcd 时，读取 `-c` 指定的本地配置文件，默认是 `./config.yaml`。
3. 环境变量覆盖文件或 etcd 中的同名配置，变量名使用 `OSS_` 前缀，例如 `OSS_SERVER_PORT`、`OSS_MYSQL_HOST`、`OSS_REDIS_ADDR`、`OSS_SECURITY_AES_KEY`。

`security.aes_key` 必须是 base64 编码的 AES key，解码后长度必须为 16、24 或 32 字节。可用下面的命令生成：

```bash
openssl rand -base64 32
```

如果需要使用自定义配置文件路径，可在启动时传入 `-c` 参数：

```bash
go run ./cmd/server/main.go -c /path/to/config.yaml
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

项目中对象和 Bucket 的 `storage_class` 均使用常量定义：
- `consts.StorageClassStandard` - 标准存储
- `consts.StorageClassIA` - 低频访问存储
- `consts.StorageClassArchive` - 归档存储

默认值均为 `STANDARD`。

## 项目架构

### 分层架构
```
HTTP Router (Hertz)
    ↓
API Handlers (api/auth/*.go)
    ↓
Business Logic (service/*/*.go)
    ↓
Data Access (adaptor/repo/*/*.go)  +  Storage (adaptor/storage/*/)  +  Cache (adaptor/redis/*/)
    ↓
External Systems (MySQL, Redis, Local Storage)
```

### 核心特性

- **完全接口化**: 所有层均定义接口，支持多实现切换
- **事务一致性**: 通过 `TxManager` 统一管理数据库事务，零 GORM 依赖泄露  
- **性能优化**: 流式处理大文件、并发控制、连接池复用
- **错误最小化**: 显式错误处理、结构化日志、可追溯错误链

### 文件结构

```
adaptor/          数据访问层：Repository、Storage、Redis、事务管理
  ├── repo/       数据库 CRUD 操作（接口 + GORM 实现）
  ├── redis/      Redis 缓存、锁、队列
  ├── storage/    文件存储（本地、云存储等）
  └── tx/         事务管理

service/          业务逻辑层：Service、DO、DTO、Converter
  ├── */service.go    业务处理
  ├── do/             领域对象
  ├── dto/            请求/响应对象
  └── converter/      类型转换

api/              HTTP 层：Handlers、中间件
  ├── auth/       各模块的 API Handlers
  └── resp.go     统一响应格式

router/           路由注册、认证中间件、权限检查
cmd/server/       应用入口、依赖注入

config/           配置管理
consts/           常量定义
common/           通用错误、工具函数
utils/            工具库（日志、连接池、加密等）
```

## 关键命令

```bash
# 生成 GORM Model 和 Query（修改数据库后执行）
go run ./tools/gen.go

# 生成类型安全的对象转换器
goverter gen ./service

# 启动开发服务（带热重载）
air

# 构建二进制
go build -o oss ./cmd/server/main.go

# 运行单元测试
go test ./...
```

## 详细文档

- [项目完整索引](PROJECT_INDEX.md) - 所有模块的详细说明
- [多部分上传指南](doc/MULTIPART_GUIDE.md) - 虚拟合并策略详解
- [对象版本设计](OBJECT_VERSIONING_DESIGN.md) - 版本控制实现
- [权限 API 文档](doc/POLICY_API.md) - Bucket Policy 详细说明
- [视频处理计划](doc/video.md) - 视频转码、HLS、加密实现
- [任务系统](doc/task.md) - 异步任务队列设计

---

**最后更新**: 2026-05-22 | **架构版本**: 1.2
