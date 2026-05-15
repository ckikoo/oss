-- ============================================================
--  OSS 完整数据库设计
--  数据库: MySQL 8.0+
--  字符集: utf8mb4
--  时间字段: DATETIME (UTC 存储)
-- ============================================================

CREATE DATABASE IF NOT EXISTS oss_db DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE oss_db;

-- ============================================================
-- 1. 用户库
-- ============================================================
CREATE TABLE  IF NOT EXISTS  users (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '用户ID',
    email           VARCHAR(128)    NOT NULL                COMMENT '邮箱',
    status          TINYINT         NOT NULL DEFAULT 1      COMMENT '1=正常 2=禁用 3=注销',
    storage_quota   BIGINT          NOT NULL DEFAULT 107374182400 COMMENT '存储配额(字节) 默认100GB',
    storage_used    BIGINT          NOT NULL DEFAULT 0      COMMENT '已用存储(字节)',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    UNIQUE KEY uk_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户表（仅账号信息，AK/SK 认证在 access_keys 表）';


-- ============================================================
-- 2. 密钥库
-- ============================================================
CREATE TABLE  IF NOT EXISTS   access_keys (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    user_id         BIGINT          NOT NULL                COMMENT '所属用户ID',
    access_key      VARCHAR(32)     NOT NULL                COMMENT 'AK 公开标识',
    secret_key      VARCHAR(256)    NOT NULL                COMMENT 'SK 加密存储(AES)',
    alias           VARCHAR(64)                             COMMENT '别名/备注',
    status          TINYINT         NOT NULL DEFAULT 1      COMMENT '1=启用 0=禁用',
    -- 权限范围: {"buckets":["b1","b2"], "actions":["GetObject","PutObject"]}
    permission      JSON                                    COMMENT '细粒度权限(NULL=全部)',
    last_used_at    DATETIME                                COMMENT '最后使用时间',
    expires_at      DATETIME                                COMMENT '过期时间(NULL=永不过期)',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    UNIQUE KEY uk_access_key (access_key),
    INDEX      idx_user_id  (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='AccessKey 密钥表';


-- ============================================================
-- 3. Bucket 库
-- ============================================================
CREATE TABLE  IF NOT EXISTS  buckets (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    user_id         BIGINT          NOT NULL                COMMENT '所属用户ID',
    name            VARCHAR(64)     NOT NULL                COMMENT 'Bucket名(全局唯一)',
    region          VARCHAR(32)     NOT NULL DEFAULT 'cn-hz' COMMENT '所在区域',
    acl             TINYINT         NOT NULL DEFAULT 0      COMMENT '0=私有 1=公共读 2=公共读写',
    versioning      TINYINT         NOT NULL DEFAULT 0      COMMENT '0=关闭 1=开启版本控制',
    status          TINYINT         NOT NULL DEFAULT 1      COMMENT '1=正常 2=锁定 3=删除',
    storage_class   VARCHAR(16)     NOT NULL DEFAULT 'STANDARD' COMMENT 'STANDARD/IA/ARCHIVE',
    object_count    BIGINT          NOT NULL DEFAULT 0      COMMENT '对象总数',
    storage_size    BIGINT          NOT NULL DEFAULT 0      COMMENT '存储总量(字节)',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    UNIQUE KEY uk_user_name (user_id, name),
    INDEX      idx_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Bucket 表';


-- ============================================================
-- 4. Object 库
-- ============================================================
CREATE TABLE  IF NOT EXISTS  objects (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    bucket_id       BIGINT          NOT NULL                COMMENT '所属BucketID',
    bucket_name     VARCHAR(64)     NOT NULL                COMMENT 'Bucket名(冗余)',
    object_key      VARCHAR(1024)   NOT NULL                COMMENT '对象路径 e.g. dir/video.mp4',
    -- ✅ 解决索引超长：存 object_key 的 MD5，用于唯一约束
    -- 应用层写入前计算: md5(object_key) 即可
    object_key_hash CHAR(32)        NOT NULL                COMMENT 'MD5(object_key) 用于唯一索引',
    version_id      VARCHAR(32)     NOT NULL DEFAULT ''     COMMENT '版本ID(未开启版本控制时为空字符串)',
    -- 存储信息
    size            BIGINT          NOT NULL DEFAULT 0      COMMENT '文件大小(字节)',
    etag            VARCHAR(64)     NOT NULL                COMMENT 'MD5 或分片合并ETag',
    content_type    VARCHAR(128)                            COMMENT 'MIME类型',
    storage_class   VARCHAR(16)     NOT NULL DEFAULT 'STANDARD',
    -- 虚拟合并关键字段
    is_multipart    TINYINT         NOT NULL DEFAULT 0      COMMENT '0=普通上传 1=分片虚拟合并',
    upload_id       VARCHAR(64)                             COMMENT '分片上传ID(is_multipart=1时)',
    storage_path    VARCHAR(512)                            COMMENT '物理路径(普通文件或物理合并后)',
    -- 访问控制
    acl             TINYINT         NOT NULL DEFAULT 0      COMMENT '0=继承Bucket 1=私有 2=公共读',
    -- 元数据
    metadata        JSON                                    COMMENT '用户自定义元数据',
    -- 状态
    status          TINYINT         NOT NULL DEFAULT 1      COMMENT '1=正常 2=删除标记(版本控制) 3=物理删除',
    access_count    BIGINT          NOT NULL DEFAULT 0      COMMENT '访问次数(懒合并触发依据)',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at      DATETIME                                COMMENT '软删除时间',

    PRIMARY KEY (id),
    -- ✅ bucket_id(8) + object_key_hash CHAR(32)×4=128 + version_id VARCHAR(32)×4=128 = 264 bytes ✅
    UNIQUE KEY uk_bucket_key_ver (bucket_id, object_key_hash, version_id),
    INDEX      idx_bucket_id     (bucket_id),
    INDEX      idx_upload_id     (upload_id),
    INDEX      idx_etag          (etag)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Object 对象表';


-- ============================================================
-- 5. 分片上传会话库
-- ============================================================
CREATE TABLE  IF NOT EXISTS  multipart_uploads (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    upload_id       VARCHAR(64)     NOT NULL                COMMENT '上传会话ID(对外暴露)',
    bucket_id       BIGINT          NOT NULL                COMMENT '所属BucketID',
    bucket_name     VARCHAR(64)     NOT NULL                COMMENT 'Bucket名(冗余)',
    object_key      VARCHAR(1024)   NOT NULL                COMMENT '目标对象路径',
    object_key_hash CHAR(32)        NOT NULL                COMMENT 'MD5(object_key) 用于索引',
    user_id         BIGINT          NOT NULL                COMMENT '发起用户ID',
    total_chunk     INT             NOT NULL                COMMENT '总分片数',
    uploaded_chunk  INT             NOT NULL DEFAULT 0      COMMENT '已上传分片数',
    status          TINYINT         NOT NULL DEFAULT 0      COMMENT '0=上传中 1=合并完成(虚拟) 2=物理合并完成 3=失败 4=已取消',
    storage_class   VARCHAR(16)                             COMMENT '存储类型',
    content_type    VARCHAR(128)                            COMMENT 'MIME类型',
    metadata        JSON                                    COMMENT '用户自定义元数据',
    expires_at      DATETIME        NOT NULL                COMMENT '会话过期时间(默认24h)',
    last_active_at  DATETIME        NOT NULL                COMMENT '最后活跃时间(用于ZSET score)',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    UNIQUE KEY uk_upload_id      (upload_id),
    -- ✅ bucket_id(8) + CHAR(32)×4=128 = 136 bytes，远低于 3072 上限
    INDEX      idx_bucket_key    (bucket_id, object_key_hash),
    INDEX      idx_user_id       (user_id),
    INDEX      idx_expires_at    (expires_at),      -- 定时清理扫描用
    INDEX      idx_last_active   (last_active_at)   -- ZSET 辅助
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='分片上传会话表';


-- ============================================================
-- 6. 分片明细库
-- ============================================================
CREATE TABLE  IF NOT EXISTS  multipart_parts (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    upload_id       VARCHAR(64)     NOT NULL                COMMENT '所属上传会话ID',
    part_number     INT             NOT NULL                COMMENT '分片序号 1~10000',
    size            BIGINT          NOT NULL                COMMENT '分片大小(字节)',
    etag            VARCHAR(64)     NOT NULL                COMMENT '分片MD5',
    storage_path    VARCHAR(512)    NOT NULL                COMMENT '分片物理存储路径',
    -- 虚拟合并后分片不删除，状态流转
    status          TINYINT         NOT NULL DEFAULT 0      COMMENT '0=上传中 1=已确认(虚拟合并) 2=物理合并完可删',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    UNIQUE KEY uk_upload_part (upload_id, part_number),
    INDEX      idx_upload_id  (upload_id),
    INDEX      idx_status     (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='分片明细表';


-- ============================================================
-- 7. 异步任务库（物理合并 / 转码 / 图片处理等）
-- ============================================================
CREATE TABLE  IF NOT EXISTS  async_tasks (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    task_id         VARCHAR(64)     NOT NULL                COMMENT '任务唯一ID',
    task_type       VARCHAR(32)     NOT NULL                COMMENT 'PHYSICAL_MERGE/TRANSCODE/IMG_PROCESS/DELETE_FILE',
    upload_id       VARCHAR(64)                             COMMENT '关联分片会话ID',
    object_id       BIGINT                                  COMMENT '关联对象ID',
    status          TINYINT         NOT NULL DEFAULT 0      COMMENT '0=待执行 1=执行中 2=完成 3=失败',
    progress        INT             NOT NULL DEFAULT 0      COMMENT '进度 0~100',
    result          JSON                                    COMMENT '执行结果',
    error_msg       TEXT                                    COMMENT '失败原因',
    retry_count     INT             NOT NULL DEFAULT 0      COMMENT '已重试次数',
    max_retry       INT             NOT NULL DEFAULT 3      COMMENT '最大重试次数',
    worker_id       VARCHAR(64)                             COMMENT '处理该任务的Worker标识',
    started_at      DATETIME                                COMMENT '开始执行时间',
    finished_at     DATETIME                                COMMENT '完成时间',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    UNIQUE KEY uk_task_id    (task_id),
    INDEX      idx_upload_id (upload_id),
    INDEX      idx_object_id (object_id),
    INDEX      idx_status    (status),
    INDEX      idx_task_type (task_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='异步任务表';


-- ============================================================
-- 8. 策略头表
-- ============================================================
CREATE TABLE  IF NOT EXISTS  bucket_policies (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '策略ID',
    bucket_id       BIGINT          NOT NULL                COMMENT '所属BucketID',
    effect          VARCHAR(8)      NOT NULL                COMMENT 'Allow / Deny',
    name            VARCHAR(64)                             COMMENT '策略名称',
    status          TINYINT         NOT NULL DEFAULT 1      COMMENT '1=启用 0=禁用',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    INDEX idx_bucket_id (bucket_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Bucket 权限策略头表';


-- ============================================================
-- 9. 策略授权主体表
-- ============================================================
CREATE TABLE  IF NOT EXISTS  policy_principals (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    policy_id       BIGINT          NOT NULL                COMMENT '策略ID',
    type            VARCHAR(16)     NOT NULL                COMMENT '主体类型(user/ak/role)',
    value           VARCHAR(128)    NOT NULL                COMMENT '主体值(如 user_id 或 ak)',

    PRIMARY KEY (id),
    INDEX idx_policy_id (policy_id),
    INDEX idx_type_value (type, value)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='策略授权主体表';


-- ============================================================
-- 10. 策略操作表
-- ============================================================
CREATE TABLE  IF NOT EXISTS  policy_actions (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    policy_id       BIGINT          NOT NULL                COMMENT '策略ID',
    action          VARCHAR(32)     NOT NULL                COMMENT '操作名(如 GetObject)',

    PRIMARY KEY (id),
    UNIQUE KEY uk_policy_action (policy_id, action),
    INDEX      idx_policy_id (policy_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='策略操作表';


-- ============================================================
-- 11. 策略资源表
-- ============================================================
CREATE TABLE  IF NOT EXISTS  policy_resources (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    policy_id       BIGINT          NOT NULL                COMMENT '策略ID',
    resource        VARCHAR(1024)   NOT NULL                COMMENT '资源路径(如 bucketName/*)',

    PRIMARY KEY (id),
    INDEX idx_policy_id (policy_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='策略资源表';


-- ============================================================
-- 12. 策略条件表
-- ============================================================
CREATE TABLE  IF NOT EXISTS  policy_conditions (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    policy_id       BIGINT          NOT NULL                COMMENT '策略ID',
    type            VARCHAR(16)     NOT NULL                COMMENT '条件类型(ip/time/etc)',
    cond_key        VARCHAR(32)                             COMMENT '条件键(start/end/...)',
    value           VARCHAR(256)    NOT NULL                COMMENT '条件值',

    PRIMARY KEY (id),
    INDEX idx_policy_id (policy_id),
    INDEX idx_type (type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='策略条件表';



-- ============================================================
-- 14. 生命周期规则库
-- ============================================================
CREATE TABLE  IF NOT EXISTS  lifecycle_rules (
    id                              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    bucket_id                       BIGINT          NOT NULL                COMMENT '所属BucketID',
    rule_name                       VARCHAR(64)     NOT NULL                COMMENT '规则名',
    status                          TINYINT         NOT NULL DEFAULT 1      COMMENT '1=启用 0=禁用',
    prefix                          VARCHAR(512)                            COMMENT '匹配对象前缀(NULL=全部)',
    -- 存储转换
    transition_days                 INT                                     COMMENT 'N天后转换存储类型',
    transition_storage_class        VARCHAR(16)                             COMMENT '目标存储类型 IA/ARCHIVE',
    -- 过期删除
    expiration_days                 INT                                     COMMENT 'N天后删除对象',
    -- 历史版本
    noncurrent_version_expiration_days INT                                  COMMENT '非当前版本N天后删除',
    -- 未完成分片清理
    abort_incomplete_multipart_days INT             NOT NULL DEFAULT 7      COMMENT '未完成分片上传N天后清理',
    created_at                      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    INDEX idx_bucket_id (bucket_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='生命周期规则表';


-- ============================================================
-- 15. 流量计量库（计费依据）
-- ============================================================
CREATE TABLE  IF NOT EXISTS  metering_daily (
    id                  BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    user_id             BIGINT          NOT NULL                COMMENT '用户ID',
    bucket_id           BIGINT                                  COMMENT 'BucketID(NULL=用户总计)',
    stat_date           DATE            NOT NULL                COMMENT '统计日期',
    -- 存储
    storage_size        BIGINT          NOT NULL DEFAULT 0      COMMENT '存储量(字节)',
    object_count        BIGINT          NOT NULL DEFAULT 0      COMMENT '对象数量',
    -- 流量
    upload_flow         BIGINT          NOT NULL DEFAULT 0      COMMENT '上行流量(字节)',
    download_flow       BIGINT          NOT NULL DEFAULT 0      COMMENT '下行流量(字节)',
    -- 请求次数
    get_request_count   BIGINT          NOT NULL DEFAULT 0      COMMENT 'GET请求次数',
    put_request_count   BIGINT          NOT NULL DEFAULT 0      COMMENT 'PUT请求次数',
    del_request_count   BIGINT          NOT NULL DEFAULT 0      COMMENT 'DELETE请求次数',

    PRIMARY KEY (id),
    UNIQUE KEY uk_user_bucket_date (user_id, bucket_id, stat_date),
    INDEX      idx_stat_date       (stat_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='流量计量日统计表';


-- ============================================================
-- 16. 操作日志库（审计）
--     ⚠️ 建议大流量时按月分表或迁移至 ClickHouse/Elasticsearch
-- ============================================================
CREATE TABLE  IF NOT EXISTS  operation_logs (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    request_id      VARCHAR(64)     NOT NULL                COMMENT '请求唯一ID',
    user_id         BIGINT                                  COMMENT '操作用户ID',
    access_key      VARCHAR(32)                             COMMENT '使用的AK',
    bucket_id       BIGINT                                  COMMENT '操作的BucketID',
    bucket_name     VARCHAR(64)                             COMMENT 'Bucket名(冗余)',
    object_key      VARCHAR(1024)                           COMMENT '操作的对象路径',
    action          VARCHAR(32)     NOT NULL                COMMENT 'PutObject/GetObject/DeleteObject/...',
    result          TINYINT         NOT NULL DEFAULT 1      COMMENT '0=失败 1=成功',
    status_code     INT             NOT NULL                COMMENT 'HTTP状态码',
    error_code      VARCHAR(64)                             COMMENT '错误码',
    client_ip       VARCHAR(64)                             COMMENT '客户端IP',
    user_agent      VARCHAR(256)                            COMMENT 'User-Agent',
    request_size    BIGINT          NOT NULL DEFAULT 0      COMMENT '请求体大小(字节)',
    response_size   BIGINT          NOT NULL DEFAULT 0      COMMENT '响应体大小(字节)',
    duration_ms     INT             NOT NULL DEFAULT 0      COMMENT '耗时(毫秒)',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    INDEX idx_user_date    (user_id, created_at),
    INDEX idx_bucket_date  (bucket_id, created_at),
    INDEX idx_action       (action),
    INDEX idx_request_id   (request_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='操作审计日志表';


-- ============================================================
-- 17. 事件通知规则库（Webhook / 回调）
-- ============================================================
CREATE TABLE  IF NOT EXISTS  event_rules (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    bucket_id       BIGINT          NOT NULL                COMMENT '所属BucketID',
    rule_name       VARCHAR(64)     NOT NULL                COMMENT '规则名',
    -- ["s3:ObjectCreated:*","s3:ObjectRemoved:Delete","s3:ObjectRestore:*"]
    events          JSON            NOT NULL                COMMENT '监听的事件类型',
    prefix          VARCHAR(512)                            COMMENT '对象前缀过滤',
    suffix          VARCHAR(128)                            COMMENT '对象后缀过滤',
    target_type     VARCHAR(16)     NOT NULL                COMMENT 'WEBHOOK/MQ/FUNC',
    target_url      VARCHAR(512)                            COMMENT '回调地址(Webhook)',
    secret          VARCHAR(128)                            COMMENT 'Webhook签名密钥',
    status          TINYINT         NOT NULL DEFAULT 1      COMMENT '1=启用 0=禁用',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    INDEX idx_bucket_id (bucket_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='事件通知规则表';


-- ============================================================
-- 18. 事件投递记录库
-- ============================================================
CREATE TABLE  IF NOT EXISTS  event_deliveries (
    id              BIGINT          NOT NULL AUTO_INCREMENT COMMENT '主键',
    rule_id         BIGINT          NOT NULL                COMMENT '关联规则ID',
    event_type      VARCHAR(64)     NOT NULL                COMMENT '事件类型',
    object_key      VARCHAR(1024)                           COMMENT '触发事件的对象',
    payload         JSON            NOT NULL                COMMENT '投递内容',
    status          TINYINT         NOT NULL DEFAULT 0      COMMENT '0=待投递 1=成功 2=失败',
    retry_count     INT             NOT NULL DEFAULT 0      COMMENT '已重试次数',
    response_code   INT                                     COMMENT 'Webhook响应码',
    response_body   TEXT                                    COMMENT 'Webhook响应体',
    next_retry_at   DATETIME                                COMMENT '下次重试时间',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    INDEX idx_rule_id      (rule_id),
    INDEX idx_status       (status),
    INDEX idx_next_retry   (next_retry_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='事件投递记录表';

-- CORS bucket 级别，一行代表一个 bucket 下允许的 origin
CREATE TABLE IF NOT EXISTS bucket_cors_rules (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,

    user_id BIGINT NOT NULL,
    bucket_name VARCHAR(128) NOT NULL,

    allowed_origin VARCHAR(255) NOT NULL,
    allowed_methods VARCHAR(128) NOT NULL,

    max_age_seconds INT NOT NULL DEFAULT 600,
    enabled TINYINT NOT NULL DEFAULT 1,

    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,

    UNIQUE KEY uk_bucket_origin (user_id, bucket_name, allowed_origin),
    INDEX idx_bucket_cors_user_bucket (user_id, bucket_name)
);
-- ============================================================
-- 外键关系备注（MVP 阶段不强制外键，业务层保证）
-- ============================================================
-- access_keys.user_id       → users.id
-- buckets.user_id           → users.id
-- objects.bucket_id         → buckets.id
-- objects.upload_id         → multipart_uploads.upload_id
-- multipart_uploads.user_id → users.id
-- multipart_uploads.bucket_id → buckets.id
-- multipart_parts.upload_id → multipart_uploads.upload_id
-- async_tasks.upload_id     → multipart_uploads.upload_id
-- async_tasks.object_id     → objects.id
-- bucket_policies.bucket_id → buckets.id
-- policy_principals.policy_id → bucket_policies.id
-- policy_actions.policy_id    → bucket_policies.id
-- policy_resources.policy_id  → bucket_policies.id
-- policy_conditions.policy_id → bucket_policies.id
-- lifecycle_rules.bucket_id → buckets.id
-- event_rules.bucket_id     → buckets.id
-- event_deliveries.rule_id  → event_rules.id


-- ============================================================
-- Redis 设计备注（配合 MySQL 使用）
-- ============================================================
-- ZSET  oss:session:expiry
--       member=upload_id  score=last_active_at(unix时间戳)
--       用途：定时扫描过期未完成的分片上传会话
--
-- HSET  oss:session:{upload_id}
--       chunk:0 ~ chunk:N  = "1"
--       用途：快速判断某分片是否已上传（避免频繁查 multipart_parts）
--
-- STRING oss:ratelimit:{user_id}:{action}
--       用途：接口限流计数器（EX 60秒）
--
-- STRING oss:lock:merge:{upload_id}
--       用途：合并任务分布式锁（SETNX + EX 300秒）