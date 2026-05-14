# Bucket Policy API 文档

## 概述

该文档详细说明 OSS 项目中的 Bucket Policy 管理接口。Bucket Policy 用于对指定 Bucket 下的对象访问行为进行细粒度控制，支持多维策略条件、主体、动作和资源匹配。

接口目前支持：

- 创建 Bucket Policy
- 列出 Bucket Policy

所有接口需通过 AK/SK 认证访问。

## 认证方式

Policy 管理接口要求 AK/SK 认证。

- **Authorization 方式**
  - `Authorization: OSS <access_key>:<timestamp>:<signature>`
  - timestamp 与服务器时间允许误差不超过 30 秒

## 路由

### 创建 Bucket Policy

- 方法：`POST`
- 路径：`/api/v1/buckets/:bucket_name/policies`
- 认证：AK/SK

### 列出 Bucket Policy

- 方法：`GET`
- 路径：`/api/v1/buckets/:bucket_name/policies`
- 认证：AK/SK

## 数据模型

### CreateBucketPolicyReq

请求体结构：

- `name` (string, required): 策略名称
- `effect` (string, optional): 策略效力，默认 `Allow`，可选 `Allow` 或 `Deny`
- `principals` ([]object, required): 策略主体列表
  - 每个主体对象包含：
    - `type` (string, required): 主体类型，如 "User", "AK"
    - `value` (string, required): 主体值，如 "user:123", "ak:access_key"，支持通配符 "*"
- `actions` ([]string, required): 允许或拒绝的动作列表，支持通配符 "*" 和路径匹配
- `resources` ([]string, required): 资源 ARN 或路径列表，支持通配符 "*" 和路径匹配
- `conditions` ([]object, optional): 策略附加条件列表
  - 每个条件对象包含：
    - `type` (string, required): 条件类型，如 "IpAddress", "NotIpAddress", "TimeRange"
    - `cond_key` (string, optional): 条件键，仅 TimeRange 类型需要，如 "start", "end"
    - `value` (string, required): 条件值
- `description` (string, optional): 策略描述

### 支持的 Actions

当前系统支持以下动作：

- `GetObject`: 下载对象
- `PutObject`: 上传对象（包括简单上传和分片上传）
- `DeleteObject`: 删除对象
- `HeadObject`: 获取对象元数据
- `ListObjects`: 列出对象
- `ListMultipartUploads`: 列出分片上传任务

### 支持的 Principals

- `User:{user_id}`: 指定用户 ID
- `AK:{access_key}`: 指定访问密钥
- `*`: 匹配所有主体

### 支持的 Resources

资源格式：`arn:oss:::{bucket_name}/{object_key}`

- `arn:oss:::{bucket_name}/*`: 匹配 bucket 下所有对象
- `arn:oss:::{bucket_name}/prefix/*`: 匹配指定前缀的对象
- `arn:oss:::{bucket_name}/{exact_key}`: 匹配精确对象
- `*`: 匹配所有资源

### 支持的 Conditions

- `IpAddress`: IP 地址范围匹配，支持 CIDR 表示法，如 "192.168.1.0/24"
- `NotIpAddress`: IP 地址排除，支持 CIDR 表示法
- `TimeRange`: 时间范围限制
  - `cond_key: "start"`: 开始时间（HH:MM 格式）
  - `cond_key: "end"`: 结束时间（HH:MM 格式）

### CreateBucketPolicyResp

- `policy_id` (int64): 新建策略 ID
- `name` (string): 策略名称
- `status` (int32): 策略状态
- `bucket_id` (int64): 关联 Bucket ID
- `created_at` (int64): 创建时间（毫秒）
- `updated_at` (int64): 更新时间（毫秒）

### BucketPolicyItem

- `policy_id` (int64)
- `bucket_id` (int64)
- `effect` (string)
- `name` (string)
- `status` (int32)
- `principals` ([]PolicyPrincipalItem)
- `actions` ([]string)
- `resources` ([]string)
- `conditions` ([]PolicyConditionItem)
- `created_at` (int64)
- `updated_at` (int64)

### ListBucketPoliciesResp

- `items` ([]BucketPolicyItem)

## 示例

### 1. 创建 Bucket Policy

请求：

```http
POST /api/v1/buckets/example-bucket/policies HTTP/1.1
Content-Type: application/json
Authorization: OSS <access_key>:<timestamp>:<signature>

{
  "name": "default-read-policy",
  "effect": "Allow",
  "principals": [
    {"type": "User", "value": "user:123"},
    {"type": "AK", "value": "ak:alice_access_key"}
  ],
  "actions": ["GetObject", "ListObjects"],
  "resources": ["arn:oss:::example-bucket/*"],
  "conditions": [
    {"type": "IpAddress", "value": "192.168.1.0/24"}
  ],
  "description": "Read-only policy for alice"
}
```

响应：

```json
{
  "policy_id": 123,
  "name": "default-read-policy",
  "status": 1,
  "bucket_id": 42,
  "created_at": 1680000000000,
  "updated_at": 1680000000000
}
```

### 2. 创建拒绝策略

```http
POST /api/v1/buckets/example-bucket/policies HTTP/1.1
Content-Type: application/json
Authorization: OSS <access_key>:<timestamp>:<signature>

{
  "name": "deny-external-access",
  "effect": "Deny",
  "principals": ["*"],
  "actions": ["PutObject", "DeleteObject"],
  "resources": ["arn:oss:::example-bucket/*"],
  "conditions": [
    {"type": "NotIpAddress", "value": "10.0.0.0/8"}
  ]
}
```

### 3. 创建时间限制策略

```http
POST /api/v1/buckets/example-bucket/policies HTTP/1.1
Content-Type: application/json
Authorization: OSS <access_key>:<timestamp>:<signature>

{
  "name": "business-hours-only",
  "effect": "Allow",
  "principals": [{"type": "User", "value": "*"}],
  "actions": ["PutObject"],
  "resources": ["arn:oss:::example-bucket/uploads/*"],
  "conditions": [
    {"type": "TimeRange", "cond_key": "start", "value": "09:00"},
    {"type": "TimeRange", "cond_key": "end", "value": "18:00"}
  ]
}
```

### 4. 列出 Bucket Policy

请求：

```http
GET /api/v1/buckets/example-bucket/policies HTTP/1.1
Authorization: OSS <access_key>:<timestamp>:<signature>
```

响应：

```json
{
  "items": [
    {
      "policy_id": 123,
      "bucket_id": 42,
      "effect": "Allow",
      "name": "default-read-policy",
      "status": 1,
      "principals": [
        {"type": "User", "value": "user:123"},
        {"type": "AK", "value": "ak:alice_access_key"}
      ],
      "actions": ["GetObject", "ListObjects"],
      "resources": ["arn:oss:::example-bucket/*"],
      "conditions": [
        {"type": "IpAddress", "value": "192.168.1.0/24"}
      ],
      "created_at": 1680000000000,
      "updated_at": 1680000000000
    }
  ]
}
```

## 策略评估逻辑

### 评估流程

当用户访问受保护资源时，系统按以下顺序评估策略：

1. **收集请求上下文**：
   - 用户 ID 和访问密钥
   - 请求动作（如 GetObject, PutObject）
   - 目标资源（如 arn:oss:::bucket/object）
   - 客户端 IP 地址

2. **策略匹配**：
   - 遍历所有活跃策略（status = 1）
   - 检查 principals 是否匹配（支持通配符）
   - 检查 actions 是否匹配（支持通配符和路径匹配）
   - 检查 resources 是否匹配（支持通配符和路径匹配）
   - 检查 conditions 是否满足（所有条件必须为真）

3. **决策逻辑**：
   - **Deny 优先**：如果任何策略匹配且 effect 为 "Deny"，立即拒绝访问
   - **Allow 累积**：收集所有匹配且 effect 为 "Allow" 的策略
   - **最终结果**：
     - 有 Allow 策略匹配 → 允许访问
     - 无任何策略匹配 → 拒绝访问

### 匹配规则

#### Principals 匹配
- `user:123` 匹配用户 ID 为 123 的请求
- `ak:access_key` 匹配指定访问密钥的请求
- `*` 匹配所有主体

#### Actions 匹配
- 精确匹配：`GetObject` 匹配 `GetObject` 请求
- 通配符：`*` 匹配所有动作
- 路径匹配：`Get*` 匹配 `GetObject`, `GetBucket` 等

#### Resources 匹配
- 精确匹配：`arn:oss:::bucket/file.txt` 匹配特定文件
- 通配符：`arn:oss:::bucket/*` 匹配 bucket 下所有对象
- 路径匹配：`arn:oss:::bucket/prefix/*` 匹配指定前缀的对象

#### Conditions 匹配
- **IpAddress**: 请求 IP 必须在指定 CIDR 范围内
- **NotIpAddress**: 请求 IP 必须不在指定 CIDR 范围内
- **TimeRange**: 当前时间必须在指定时间范围内（HH:MM 格式）

### 性能优化

- 使用 `oss/utils/pool` 控制并发查询子表，避免 N+1 查询问题
- 策略缓存到 Redis 中，减少数据库查询
- 支持批量策略评估，减少单次请求的数据库访问

## 表关系

- `bucket_policies` 关联 `bucket_id`
- 子表 `policy_principals`、`policy_actions`、`policy_resources`、`policy_conditions` 均关联 `policy_id`

## 注意事项

- `effect` 仅支持 `Allow` 和 `Deny`，默认值为 `Allow`
- `principals`、`actions`、`resources` 为必填字段
- `conditions` 为可选字段，若存在需提供完整条件结构
- `created_at` 和 `updated_at` 均以毫秒时间戳返回
- 策略名称在同一 bucket 内必须唯一
- 支持通配符 `*` 用于 principals、actions 和 resources
- Deny 策略优先级高于 Allow 策略
- 所有条件必须同时满足（AND 关系）
- TimeRange 条件使用 24 小时制 HH:MM 格式
- IP 地址条件支持 CIDR 表示法
- 策略状态为 1 时生效，0 时禁用

## 错误处理

### 常见错误码

- `400 Bad Request`: 请求参数错误，如必填字段缺失、格式不正确
- `401 Unauthorized`: 认证失败，Authorization 头无效
- `403 Forbidden`: 策略拒绝访问
- `404 Not Found`: Bucket 不存在
- `409 Conflict`: 策略名称在同一 bucket 内重复
- `500 Internal Server Error`: 服务器内部错误

### 错误响应格式

```json
{
  "code": 400,
  "msg": "param error: principals is required",
  "data": null
}
```
