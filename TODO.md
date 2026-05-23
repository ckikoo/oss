# TODO

本文件记录 OSS 项目的后续拓展方向。当前项目核心功能已经比较完整，后续重点建议放在兼容性、可运维性、安全治理和产品化体验上。

## P0 - 先处理

### 配置与密钥治理

- [x] 新增 `config.example.yaml`，只保留示例配置，不包含真实密码、地址和密钥。
- [x] 支持通过环境变量覆盖配置，例如 MySQL、Redis、AES Key、Server Port。
- [x] 启动时校验关键配置：
  - [x] AES Key 是否为合法 base64。
  - [x] AES Key 解码后长度是否满足加密要求。
  - [x] MySQL / Redis 地址是否为空。
  - [x] 生产环境是否禁用默认弱配置。
- [x] 将本地开发配置与生产配置分离，避免敏感配置进入仓库。
- [x] README 补充配置加载优先级说明。

### 健康检查

- [x] 增加 `GET /healthz`，用于进程存活检查。
- [x] 增加 `GET /readyz`，检查 MySQL、Redis、存储目录是否可用。
-[x] 健康检查返回结构化结果，方便部署系统识别问题。

### 基础可观测性

- [x] 统计 HTTP 请求数、状态码、接口耗时。
- [ ] 统计上传/下载字节数。
- [ ] 统计异步任务积压、运行中、失败数量。
- [ ] 统计视频转码成功率、失败率和耗时。

## P1 - 强烈建议

### S3 兼容 API

- [ ] 设计 S3 兼容路由层，不破坏现有 `/api/v1` API。
- [ ] 支持基础 Bucket API：
  - [ ] `CreateBucket`
  - [ ] `DeleteBucket`
  - [ ] `HeadBucket`
  - [ ] `ListBuckets`
- [ ] 支持基础 Object API：
  - [ ] `PutObject`
  - [ ] `GetObject`
  - [ ] `HeadObject`
  - [ ] `DeleteObject`
  - [ ] `CopyObject`
- [ ] 支持 `ListObjectsV2`：
  - [ ] `prefix`
  - [ ] `delimiter`
  - [ ] `continuation-token`
  - [ ] `max-keys`
  - [ ] `start-after`
- [ ] 支持 S3 Multipart Upload：
  - [ ] `CreateMultipartUpload`
  - [ ] `UploadPart`
  - [ ] `ListParts`
  - [ ] `CompleteMultipartUpload`
  - [ ] `AbortMultipartUpload`
- [ ] 增加 AWS Signature V4 认证兼容。
- [ ] 使用 `aws-cli`、`mc`、`rclone` 做兼容性测试。

### Object 列表增强

- [ ] 现有对象列表接口支持 `prefix` 过滤。
- [ ] 支持 `delimiter` 模拟目录层级。
- [ ] 支持分页游标，避免大 Bucket 列表查询压力过大。
- [ ] 支持按 `storage_class` 过滤。
- [ ] 支持按 `content_type` 过滤。
- [ ] 支持按创建时间范围过滤。
- [ ] 为列表查询补充索引设计和压测结果。

### 异步任务管理

- [ ] 增加任务查询 API。
- [ ] 增加失败任务重试 API。
- [ ] 增加任务取消 API。
- [ ] 增加任务死信状态或死信队列。
- [ ] 增加 worker 心跳记录。
- [ ] 增加任务运行耗时、失败原因、重试次数查询。
- [ ] 给生命周期、事件投递、视频转码统一任务观测模型。

## P2 - 产品化增强

### 存储后端扩展

- [ ] 梳理 `adaptor/storage` 接口能力边界。
- [ ] 增加本地多目录存储策略。
- [ ] 增加 S3 / MinIO 存储后端。
- [ ] 增加阿里 OSS / 腾讯 COS 适配器。
- [ ] 支持按 Bucket 配置默认存储后端。
- [ ] 生命周期转 IA / ARCHIVE 时支持迁移到不同存储后端。
- [ ] 补充存储后端一致性和失败补偿策略。

### 安全能力增强

- [ ] 支持 AccessKey 轮换。
- [ ] 支持 AccessKey 过期前告警。
- [ ] 支持 IP 白名单。
- [ ] 支持请求防重放窗口配置。
- [ ] 支持预签名 Token 绑定 Content-MD5。
- [ ] 支持一次性 Token。
- [ ] 增强 Bucket Policy 条件表达式：
  - [ ] IP 条件。
  - [ ] 时间条件。
  - [ ] UserAgent 条件。
  - [ ] Object 标签条件。

### 管理控制台

- [ ] 增加 Web 管理端项目。
- [ ] 支持 AccessKey 管理。
- [ ] 支持 Bucket / Object 浏览。
- [ ] 支持上传、下载、删除对象。
- [ ] 支持版本历史查看和恢复。
- [ ] 支持 Policy / CORS / Lifecycle / Event 配置。
- [ ] 支持审计日志查询。
- [ ] 支持流量统计图表。
- [ ] 支持视频转码状态查看和播放测试。

## P3 - 体验和工程质量

### 文档整理

- [ ] 清理 README 中重复的快速开始和存储类型说明。
- [ ] 补齐 API 示例请求和响应。
- [ ] 为核心流程增加时序图：
  - [ ] 普通上传。
  - [ ] 分片上传。
  - [ ] 下载 Token。
  - [ ] 生命周期执行。
  - [ ] 事件投递。
  - [ ] 视频转码。
- [ ] 增加部署文档：
  - [ ] Docker Compose。
  - [ ] 单机部署。
  - [ ] 生产配置建议。

### 测试与质量

- [ ] 增加 API 集成测试。
- [ ] 增加鉴权和 Policy 决策表测试。
- [ ] 增加 Multipart 并发上传测试。
- [ ] 增加生命周期扫描和执行测试。
- [ ] 增加事件投递重试测试。
- [ ] 增加对象版本控制边界测试。
- [ ] 增加 `go test -race ./...` 到 CI。
- [ ] 增加基础 benchmark，覆盖对象列表、Policy 评估、分片读取。

### 部署与 CI

- [ ] 增加 GitHub Actions / CI 配置。
- [ ] CI 执行 `go test ./...`。
- [ ] CI 执行 `go build ./...`。
- [ ] CI 执行格式化检查。
- [ ] 增加 Dockerfile。
- [ ] 完善 `compose.yml`，包含 MySQL、Redis、服务本体。
- [ ] 支持初始化数据库脚本自动执行。

## 近期推荐路线

1. 先做配置安全化和 `config.example.yaml`，降低项目泄露敏感信息的风险。
2. 再补 `/healthz`、`/readyz`、`/metrics`，让服务更适合部署和排障。
3. 接着增强对象列表能力，为 S3 `ListObjectsV2` 打基础。
4. 最后启动 S3 兼容层，这是项目价值提升最大的长期方向。
