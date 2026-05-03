# Bucket Policy API 文档

## 概述

该文档详细说明 OSS 项目中的 Bucket Policy 管理接口。Bucket Policy 用于对指定 Bucket 下的对象访问行为进行细粒度控制，支持多维策略条件、主体、动作和资源匹配。

接口目前支持：

- 创建 Bucket Policy
- 列出 Bucket Policy

所有接口需通过 AK/SK 认证访问。

## 认证方式

支持两种 AK/SK 认证方式：

- Header 方式
  - `X-Access-Key`: Access Key
  - `X-Secret-Key`: Secret Key
- Authorization 方式
  - `Authorization: AccessKey AK:SK`

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
    - `type` (string, required)
    - `value` (string, required)
- `actions` ([]string, required): 允许或拒绝的动作列表
- `resources` ([]string, required): 资源 ARN 或路径列表
- `conditions` ([]object, optional): 策略附加条件列表
  - 每个条件对象包含：
    - `type` (string, required)
    - `cond_key` (string, optional)
    - `value` (string, required)
- `description` (string, optional): 策略描述

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
Authorization: AccessKey AK:SK

{
  "name": "default-read-policy",
  "effect": "Allow",
  "principals": [
    {"type": "User", "value": "user:alice"}
  ],
  "actions": ["oss:GetObject", "oss:ListObjects"],
  "resources": ["arn:oss:::example-bucket/*"],
  "conditions": [
    {"type": "IpAddress", "cond_key": "aws:SourceIp", "value": "192.0.2.0/24"}
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

### 2. 列出 Bucket Policy

请求：

```http
GET /api/v1/buckets/example-bucket/policies HTTP/1.1
Authorization: AccessKey AK:SK
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
        {"type": "User", "value": "user:alice"}
      ],
      "actions": ["oss:GetObject", "oss:ListObjects"],
      "resources": ["arn:oss:::example-bucket/*"],
      "conditions": [
        {"type": "IpAddress", "cond_key": "aws:SourceIp", "value": "192.0.2.0/24"}
      ],
      "created_at": 1680000000000,
      "updated_at": 1680000000000
    }
  ]
}
```

## 实现细节

- 创建策略时，服务层会先校验参数，并将策略头表和子表写入数据库。
- 子表包括：
  - `policy_principals`
  - `policy_actions`
  - `policy_resources`
  - `policy_conditions`
- 列表查询时，先读取 `bucket_policies` 头表，再并发读取上述子表数据，最终组装成完整策略对象。
- 并发控制：
  - 使用 `oss/utils/pool` 控制 `ListBucketPolicies` 的并发任务数量
  - 避免策略数较多时出现过度串行查询和连接压力

## 表关系

- `bucket_policies` 关联 `bucket_id`
- 子表 `policy_principals`、`policy_actions`、`policy_resources`、`policy_conditions` 均关联 `policy_id`

## 注意事项

- `effect` 仅支持 `Allow` 和 `Deny`
- `principals`、`actions`、`resources` 为必填字段
- `conditions` 为可选字段，若存在需提供完整条件结构
- `created_at` 和 `updated_at` 均以毫秒时间戳返回
