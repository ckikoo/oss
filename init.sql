-- ============================================================
--  OSS 完整数据库设计
--  数据库: MySQL 8.0+
--  字符集: utf8mb4
--  时间字段: DATETIME (UTC 存储)
-- ============================================================

CREATE DATABASE IF NOT EXISTS oss_db DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE oss_db;

-- 关闭外键检查，确保 DROP / CREATE 顺序不受约束限制
SET FOREIGN_KEY_CHECKS = 0;

-- ============================================================
--  DROP TABLE（逆依赖顺序：子表先删）
-- ============================================================
DROP TABLE IF EXISTS `video_encrypt_keys`;
DROP TABLE IF EXISTS `video_transcode_profiles`;
DROP TABLE IF EXISTS `video_transcodes`;
DROP TABLE IF EXISTS `operation_logs`;
DROP TABLE IF EXISTS `metering_daily`;
DROP TABLE IF EXISTS `async_tasks`;
DROP TABLE IF EXISTS `objects`;
DROP TABLE IF EXISTS `multipart_parts`;
DROP TABLE IF EXISTS `multipart_uploads`;
DROP TABLE IF EXISTS `event_deliveries`;
DROP TABLE IF EXISTS `event_rules`;
DROP TABLE IF EXISTS `lifecycle_rules`;
DROP TABLE IF EXISTS `policy_resources`;
DROP TABLE IF EXISTS `policy_principals`;
DROP TABLE IF EXISTS `policy_conditions`;
DROP TABLE IF EXISTS `policy_actions`;
DROP TABLE IF EXISTS `bucket_policies`;
DROP TABLE IF EXISTS `bucket_cors_rules`;
DROP TABLE IF EXISTS `buckets`;
DROP TABLE IF EXISTS `access_keys`;
DROP TABLE IF EXISTS `users`;

-- ============================================================
--  CREATE TABLE（正依赖顺序：父表先建）
-- ============================================================

-- ----------------------------
-- 1. users（无依赖，最顶层）
-- ----------------------------
CREATE TABLE `users`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '用户ID',
  `email` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '邮箱',
  `status` tinyint NOT NULL DEFAULT 1 COMMENT '1=正常 2=禁用 3=注销',
  `storage_quota` bigint NOT NULL DEFAULT 107374182400 COMMENT '存储配额(字节) 默认100GB',
  `storage_used` bigint NOT NULL DEFAULT 0 COMMENT '已用存储(字节)',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_email`(`email` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 2 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = '用户表（仅账号信息，AK/SK 认证在 access_keys 表）' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 2. access_keys（→ users）
-- ----------------------------
CREATE TABLE `access_keys`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `user_id` bigint NOT NULL COMMENT '所属用户ID',
  `access_key` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'AK 公开标识',
  `secret_key` varchar(256) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'SK 加密存储(AES)',
  `alias` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '别名/备注',
  `status` tinyint NOT NULL DEFAULT 1 COMMENT '1=启用 0=禁用',
  `permission` json NULL COMMENT '细粒度权限(NULL=全部)',
  `last_used_at` datetime NULL DEFAULT NULL COMMENT '最后使用时间',
  `expires_at` datetime NULL DEFAULT NULL COMMENT '过期时间(NULL=永不过期)',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_access_key`(`access_key` ASC) USING BTREE,
  INDEX `idx_user_id`(`user_id` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 20 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = 'AccessKey 密钥表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 3. buckets（→ users）
-- ----------------------------
CREATE TABLE `buckets`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `user_id` bigint NOT NULL COMMENT '所属用户ID',
  `name` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'Bucket名(全局唯一)',
  `region` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT 'cn-hz' COMMENT '所在区域',
  `acl` tinyint NOT NULL DEFAULT 0 COMMENT '1=私有 2=公共读 3=公共读写',
  `versioning` tinyint NOT NULL DEFAULT 0 COMMENT '1=关闭 2=开启版本控制',
  `status` tinyint NOT NULL DEFAULT 1 COMMENT '1=正常 2=锁定 3=删除',
  `storage_class` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT 'STANDARD' COMMENT 'STANDARD/IA/ARCHIVE',
  `object_count` bigint NOT NULL DEFAULT 0 COMMENT '对象总数',
  `storage_size` bigint NOT NULL DEFAULT 0 COMMENT '存储总量(字节)',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_name`(`name` ASC) USING BTREE,
  INDEX `idx_user_id`(`user_id` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 9 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = 'Bucket 表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 4. bucket_cors_rules（→ buckets）
-- ----------------------------
CREATE TABLE `bucket_cors_rules`  (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `user_id` bigint NOT NULL,
  `bucket_name` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `allowed_origin` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `allowed_methods` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `max_age_seconds` int NOT NULL DEFAULT 600,
  `enabled` tinyint NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_bucket_origin`(`user_id` ASC, `bucket_name` ASC, `allowed_origin` ASC) USING BTREE,
  INDEX `idx_bucket_cors_user_bucket`(`user_id` ASC, `bucket_name` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 1 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_bin ROW_FORMAT = Dynamic;

-- ----------------------------
-- 5. bucket_policies（→ buckets）
-- ----------------------------
CREATE TABLE `bucket_policies`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '策略ID',
  `bucket_id` bigint NOT NULL COMMENT '所属BucketID',
  `effect` varchar(8) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'Allow / Deny',
  `name` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '策略名称',
  `status` tinyint NOT NULL DEFAULT 1 COMMENT '1=启用 0=禁用',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`) USING BTREE,
  INDEX `idx_bucket_id`(`bucket_id` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 1 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = 'Bucket 权限策略头表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 6. policy_actions（→ bucket_policies）
-- ----------------------------
CREATE TABLE `policy_actions`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `policy_id` bigint NOT NULL COMMENT '策略ID',
  `action` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '操作名(如 GetObject)',
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_policy_action`(`policy_id` ASC, `action` ASC) USING BTREE,
  INDEX `idx_policy_id`(`policy_id` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 1 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = '策略操作表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 7. policy_conditions（→ bucket_policies）
-- ----------------------------
CREATE TABLE `policy_conditions`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `policy_id` bigint NOT NULL COMMENT '策略ID',
  `type` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '条件类型(ip/time/etc)',
  `cond_key` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '条件键(start/end/...)',
  `value` varchar(256) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '条件值',
  PRIMARY KEY (`id`) USING BTREE,
  INDEX `idx_policy_id`(`policy_id` ASC) USING BTREE,
  INDEX `idx_type`(`type` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 1 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = '策略条件表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 8. policy_principals（→ bucket_policies）
-- ----------------------------
CREATE TABLE `policy_principals`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `policy_id` bigint NOT NULL COMMENT '策略ID',
  `type` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '主体类型(user/ak/role)',
  `value` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '主体值(如 user_id 或 ak)',
  PRIMARY KEY (`id`) USING BTREE,
  INDEX `idx_policy_id`(`policy_id` ASC) USING BTREE,
  INDEX `idx_type_value`(`type` ASC, `value` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 1 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = '策略授权主体表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 9. policy_resources（→ bucket_policies）
-- ----------------------------
CREATE TABLE `policy_resources`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `policy_id` bigint NOT NULL COMMENT '策略ID',
  `resource` varchar(1024) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '资源路径(如 bucketName/*)',
  PRIMARY KEY (`id`) USING BTREE,
  INDEX `idx_policy_id`(`policy_id` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 1 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = '策略资源表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 10. lifecycle_rules（→ buckets）
-- ----------------------------
CREATE TABLE `lifecycle_rules`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `bucket_id` bigint NOT NULL COMMENT '所属BucketID',
  `rule_name` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '规则名',
  `status` tinyint NOT NULL DEFAULT 1 COMMENT '1=启用 0=禁用',
  `prefix` varchar(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '匹配对象前缀(NULL=全部)',
  `transition_days` int NULL DEFAULT NULL COMMENT 'N天后转换存储类型',
  `transition_storage_class` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '目标存储类型 IA/ARCHIVE',
  `expiration_days` int NULL DEFAULT NULL COMMENT 'N天后删除对象',
  `noncurrent_version_expiration_days` int NULL DEFAULT NULL COMMENT '非当前版本N天后删除',
  `abort_incomplete_multipart_days` int NOT NULL DEFAULT 7 COMMENT '未完成分片上传N天后清理',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`) USING BTREE,
  INDEX `idx_bucket_id`(`bucket_id` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 46 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = '生命周期规则表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 11. event_rules（→ buckets）
-- ----------------------------
CREATE TABLE `event_rules`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `bucket_id` bigint NOT NULL COMMENT '所属BucketID',
  `rule_name` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '规则名',
  `events` json NOT NULL COMMENT '监听的事件类型',
  `prefix` varchar(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '对象前缀过滤',
  `suffix` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '对象后缀过滤',
  `target_type` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'WEBHOOK/MQ/FUNC',
  `target_url` varchar(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '回调地址(Webhook)',
  `secret` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT 'Webhook签名密钥',
  `status` tinyint NOT NULL DEFAULT 1 COMMENT '1=启用 0=禁用',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`) USING BTREE,
  INDEX `idx_bucket_id`(`bucket_id` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 1 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = '事件通知规则表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 12. event_deliveries（→ event_rules）
-- ----------------------------
CREATE TABLE `event_deliveries`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `rule_id` bigint NOT NULL COMMENT '关联规则ID',
  `event_type` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '事件类型',
  `object_key` varchar(1024) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '触发事件的对象',
  `payload` json NOT NULL COMMENT '投递内容',
  `status` tinyint NOT NULL DEFAULT 0 COMMENT '0=待投递 1=成功 2=失败 3=投递中',
  `retry_count` int NOT NULL DEFAULT 0 COMMENT '已重试次数',
  `response_code` int NULL DEFAULT NULL COMMENT 'Webhook响应码',
  `response_body` text CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL COMMENT 'Webhook响应体',
  `next_retry_at` datetime NULL DEFAULT NULL COMMENT '下次重试时间',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`) USING BTREE,
  INDEX `idx_rule_id`(`rule_id` ASC) USING BTREE,
  INDEX `idx_status`(`status` ASC) USING BTREE,
  INDEX `idx_next_retry`(`next_retry_at` ASC) USING BTREE,
  INDEX `idx_event_deliveries_status_retry`(`status` ASC, `next_retry_at` ASC, `id` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 1 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = '事件投递记录表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 13. multipart_uploads（→ users, buckets）
-- ----------------------------
CREATE TABLE `multipart_uploads`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `upload_id` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '上传会话ID(对外暴露)',
  `bucket_id` bigint NOT NULL COMMENT '所属BucketID',
  `bucket_name` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'Bucket名(冗余)',
  `object_key` varchar(1024) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '目标对象路径',
  `object_key_hash` char(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'MD5(object_key) 用于索引',
  `user_id` bigint NOT NULL COMMENT '发起用户ID',
  `total_chunk` int NOT NULL COMMENT '总分片数',
  `status` tinyint NOT NULL DEFAULT 0 COMMENT '0=上传中 1=合并完成(虚拟) 2=物理合并完成 3=失败 4=已取消',
  `storage_class` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '存储类型',
  `content_type` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT 'MIME类型',
  `metadata` json NULL COMMENT '用户自定义元数据',
  `expires_at` datetime NOT NULL COMMENT '会话过期时间(默认24h)',
  `last_active_at` datetime NOT NULL COMMENT '最后活跃时间(用于ZSET score)',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `version_id` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL,
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_upload_id`(`upload_id` ASC) USING BTREE,
  INDEX `idx_bucket_key`(`bucket_id` ASC, `object_key_hash` ASC) USING BTREE,
  INDEX `idx_user_id`(`user_id` ASC) USING BTREE,
  INDEX `idx_expires_at`(`expires_at` ASC) USING BTREE,
  INDEX `idx_last_active`(`last_active_at` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 124 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = '分片上传会话表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 14. multipart_parts（→ multipart_uploads）
-- ----------------------------
CREATE TABLE `multipart_parts`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `upload_id` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '所属上传会话ID',
  `part_number` int NOT NULL COMMENT '分片序号 1~10000',
  `size` bigint NOT NULL COMMENT '分片大小(字节)',
  `etag` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '分片MD5',
  `status` tinyint NOT NULL DEFAULT 0 COMMENT '0=上传中 1=已确认(虚拟合并) 2=物理合并完可删',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_upload_part`(`upload_id` ASC, `part_number` ASC) USING BTREE,
  INDEX `idx_upload_id`(`upload_id` ASC) USING BTREE,
  INDEX `idx_status`(`status` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 1283 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = '分片明细表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 15. objects（→ buckets, multipart_uploads）
-- ----------------------------
CREATE TABLE `objects`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `bucket_id` bigint NOT NULL COMMENT '所属 Bucket ID',
  `bucket_name` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'Bucket 名冗余',
  `parent_id` bigint NULL DEFAULT NULL COMMENT '父目录对象 ID，根层级为空',
  `object_key` varchar(1024) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '对象路径',
  `object_key_hash` char(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'MD5(object_key)，用于索引和唯一约束',
  `version_id` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '版本 ID，建议每次写入都生成',
  `size` bigint NOT NULL DEFAULT 0 COMMENT '对象大小，delete marker 为 0',
  `etag` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '' COMMENT 'ETag',
  `content_type` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT 'MIME 类型',
  `storage_class` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT 'STANDARD' COMMENT 'STANDARD/IA/ARCHIVE',
  `is_multipart` tinyint NOT NULL DEFAULT 0 COMMENT '0=普通对象 1=分片虚拟合并对象',
  `upload_id` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '分片上传 ID',
  `storage_path` varchar(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '物理存储路径，delete marker 为空',
  `acl` tinyint NOT NULL DEFAULT 0 COMMENT '0=继承Bucket 1=私有 2=公共读',
  `metadata` json NULL COMMENT '用户自定义元数据',
  `is_dir` tinyint NOT NULL DEFAULT 0 COMMENT '0=对象 1=目录',
  `is_latest` tinyint NOT NULL DEFAULT 0 COMMENT '0=历史版本 1=当前最新版本',
  `status` tinyint NOT NULL DEFAULT 1 COMMENT '1=正常 2=删除标记 3=永久删除',
  `access_count` bigint NOT NULL DEFAULT 0 COMMENT '访问次数',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `deleted_at` datetime NULL DEFAULT NULL COMMENT '永久删除时间',
  `latest_guard` char(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci GENERATED ALWAYS AS ((case when ((`is_latest` = 1) and (`status` <> 3)) then `object_key_hash` else NULL end)) STORED COMMENT '保证同一对象只有一个 latest' NULL,
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_bucket_key_ver`(`bucket_id` ASC, `object_key_hash` ASC, `version_id` ASC) USING BTREE,
  UNIQUE INDEX `uk_object_latest`(`bucket_id` ASC, `latest_guard` ASC) USING BTREE,
  INDEX `idx_objects_key_lookup`(`bucket_name` ASC, `object_key`(255) ASC, `version_id` ASC, `is_latest` ASC, `status` ASC) USING BTREE,
  INDEX `idx_objects_parent_list`(`bucket_id` ASC, `parent_id` ASC, `is_latest` ASC, `status` ASC, `is_dir` DESC, `object_key`(255) ASC, `id` ASC) USING BTREE,
  INDEX `idx_objects_bucket_scan`(`bucket_id` ASC, `status` ASC, `is_latest` ASC, `id` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 16 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = 'Object 对象版本表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 16. async_tasks（→ multipart_uploads, objects）
-- ----------------------------
CREATE TABLE `async_tasks`  (
  `id` bigint UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '任务ID，Redis LIST item 使用该值',
  `user_id` bigint NOT NULL DEFAULT 0 COMMENT '用户ID',
  `task_type` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT '任务类型，如 PHYSICAL_MERGE/ABORT_MULTIPART',
  `biz_type` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '' COMMENT '业务对象类型，如 upload/object/event',
  `biz_id` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT '业务幂等ID，如 upload_id/object_version_id/delivery_id',
  `status` tinyint NOT NULL DEFAULT 0 COMMENT '0=pending 1=queued 2=running 3=completed 4=failed 5=canceled 6=dead_letter',
  `progress` tinyint UNSIGNED NOT NULL DEFAULT 0 COMMENT '进度 0~100',
  `retry_count` int NOT NULL DEFAULT 0 COMMENT '已重试次数',
  `max_retry` int NOT NULL DEFAULT 3 COMMENT '最大重试次数',
  `result` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NULL COMMENT '执行结果',
  `last_error` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NULL COMMENT '最近一次失败原因',
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_async_tasks_biz`(`task_type` ASC, `biz_id` ASC) USING BTREE,
  INDEX `idx_async_tasks_status_id`(`status` ASC, `id` ASC) USING BTREE,
  INDEX `idx_async_tasks_user_status`(`user_id` ASC, `status` ASC) USING BTREE,
  INDEX `idx_async_tasks_status_updated_at`(`status` ASC, `updated_at` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 4 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_unicode_ci COMMENT = '异步任务表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 17. metering_daily（→ users, buckets）
-- ----------------------------
CREATE TABLE `metering_daily`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `user_id` bigint NOT NULL COMMENT '用户ID',
  `bucket_id` bigint NULL DEFAULT NULL COMMENT 'BucketID(NULL=用户总计)',
  `stat_date` date NOT NULL COMMENT '统计日期',
  `storage_size` bigint NOT NULL DEFAULT 0 COMMENT '存储量(字节)',
  `object_count` bigint NOT NULL DEFAULT 0 COMMENT '对象数量',
  `upload_flow` bigint NOT NULL DEFAULT 0 COMMENT '上行流量(字节)',
  `download_flow` bigint NOT NULL DEFAULT 0 COMMENT '下行流量(字节)',
  `get_request_count` bigint NOT NULL DEFAULT 0 COMMENT 'GET请求次数',
  `put_request_count` bigint NOT NULL DEFAULT 0 COMMENT 'PUT请求次数',
  `del_request_count` bigint NOT NULL DEFAULT 0 COMMENT 'DELETE请求次数',
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_user_bucket_date`(`user_id` ASC, `bucket_id` ASC, `stat_date` ASC) USING BTREE,
  INDEX `idx_stat_date`(`stat_date` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 97 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = '流量计量日统计表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 18. operation_logs（→ users, buckets）
-- ----------------------------
CREATE TABLE `operation_logs`  (
  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  `request_id` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '请求唯一ID',
  `user_id` bigint NULL DEFAULT NULL COMMENT '操作用户ID',
  `access_key` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '使用的AK',
  `bucket_id` bigint NULL DEFAULT NULL COMMENT '操作的BucketID',
  `bucket_name` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT 'Bucket名(冗余)',
  `object_key` varchar(1024) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '操作的对象路径',
  `action` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'PutObject/GetObject/DeleteObject/...',
  `result` tinyint NOT NULL DEFAULT 1 COMMENT '0=失败 1=成功',
  `status_code` int NOT NULL COMMENT 'HTTP状态码',
  `error_code` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '错误码',
  `client_ip` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '客户端IP',
  `user_agent` varchar(256) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT 'User-Agent',
  `request_size` bigint NOT NULL DEFAULT 0 COMMENT '请求体大小(字节)',
  `response_size` bigint NOT NULL DEFAULT 0 COMMENT '响应体大小(字节)',
  `duration_ms` int NOT NULL DEFAULT 0 COMMENT '耗时(毫秒)',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`) USING BTREE,
  INDEX `idx_user_date`(`user_id` ASC, `created_at` ASC) USING BTREE,
  INDEX `idx_bucket_date`(`bucket_id` ASC, `created_at` ASC) USING BTREE,
  INDEX `idx_action`(`action` ASC) USING BTREE,
  INDEX `idx_request_id`(`request_id` ASC) USING BTREE
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT = '操作审计日志表' ROW_FORMAT = Dynamic;

-- ----------------------------
-- 19. video_transcodes（→ users, buckets, objects）
-- ----------------------------
CREATE TABLE `video_transcodes`  (
  `id` bigint UNSIGNED NOT NULL AUTO_INCREMENT,
  `user_id` bigint NOT NULL,
  `bucket_id` bigint NOT NULL,
  `bucket_name` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `object_id` bigint UNSIGNED NOT NULL,
  `object_key` varchar(1024) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `object_key_hash` char(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `version_id` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `source_etag` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `source_size` bigint NOT NULL DEFAULT 0,
  `status` tinyint NOT NULL DEFAULT 0 COMMENT '0=pending 1=processing 2=done 3=failed 4=deleted',
  `duration_ms` bigint NOT NULL DEFAULT 0,
  `derived_size` bigint NOT NULL DEFAULT 0,
  `profile_count` int NOT NULL DEFAULT 0,
  `done_profile_count` int NOT NULL DEFAULT 0,
  `last_error` text CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `finished_at` datetime(3) NULL DEFAULT NULL,
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_video_transcodes_object_version`(`object_id` ASC, `version_id` ASC) USING BTREE,
  INDEX `idx_video_transcodes_user_status`(`user_id` ASC, `status` ASC) USING BTREE,
  INDEX `idx_video_transcodes_bucket_object`(`bucket_id` ASC, `object_key_hash` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 1 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_bin ROW_FORMAT = Dynamic;

-- ----------------------------
-- 20. video_transcode_profiles（→ video_transcodes）
-- ----------------------------
CREATE TABLE `video_transcode_profiles`  (
  `id` bigint UNSIGNED NOT NULL AUTO_INCREMENT,
  `transcode_id` bigint UNSIGNED NOT NULL,
  `profile` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT '1080p/720p/480p/360p/origin',
  `status` tinyint NOT NULL DEFAULT 0 COMMENT '0=pending 1=processing 2=done 3=failed 4=deleted',
  `video_bitrate` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `audio_bitrate` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `width` int NOT NULL DEFAULT 0,
  `fps` int NOT NULL DEFAULT 0,
  `height` int(10) UNSIGNED ZEROFILL NOT NULL DEFAULT 0000000000,
  `asset_prefix` varchar(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `playlist_key` varchar(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `size` bigint NOT NULL DEFAULT 0,
  `segment_count` int NOT NULL DEFAULT 0,
  `duration_ms` bigint NOT NULL DEFAULT 0,
  `last_error` text CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
  `started_at` datetime(3) NULL DEFAULT NULL,
  `finished_at` datetime(3) NULL DEFAULT NULL,
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_video_profiles_transcode_profile`(`transcode_id` ASC, `profile` ASC) USING BTREE,
  INDEX `idx_video_profiles_status_updated`(`status` ASC, `updated_at` ASC) USING BTREE
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_bin ROW_FORMAT = Dynamic;

-- ----------------------------
-- 21. video_encrypt_keys（→ video_transcodes, video_transcode_profiles）
-- ----------------------------
CREATE TABLE `video_encrypt_keys`  (
  `id` bigint UNSIGNED NOT NULL AUTO_INCREMENT,
  `transcode_id` bigint UNSIGNED NOT NULL,
  `profile_id` bigint UNSIGNED NOT NULL,
  `key_id` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `encrypted_key` varbinary(512) NOT NULL,
  `algorithm` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT 'HLS-AES-128',
  `key_version` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `kms_key_id` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE INDEX `uk_video_encrypt_keys_key_id`(`key_id` ASC) USING BTREE,
  UNIQUE INDEX `uk_video_encrypt_keys_profile`(`profile_id` ASC) USING BTREE
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_bin ROW_FORMAT = Dynamic;

-- 恢复外键检查
SET FOREIGN_KEY_CHECKS = 1;

-- ============================================================
-- 外键关系备注（MVP 阶段不强制外键，业务层保证）
-- ============================================================
-- access_keys.user_id         → users.id
-- buckets.user_id             → users.id
-- bucket_cors_rules.bucket_name → buckets.name
-- bucket_policies.bucket_id   → buckets.id
-- policy_actions.policy_id    → bucket_policies.id
-- policy_conditions.policy_id → bucket_policies.id
-- policy_principals.policy_id → bucket_policies.id
-- policy_resources.policy_id  → bucket_policies.id
-- lifecycle_rules.bucket_id   → buckets.id
-- event_rules.bucket_id       → buckets.id
-- event_deliveries.rule_id    → event_rules.id
-- multipart_uploads.user_id   → users.id
-- multipart_uploads.bucket_id → buckets.id
-- multipart_parts.upload_id   → multipart_uploads.upload_id
-- objects.bucket_id           → buckets.id
-- objects.upload_id           → multipart_uploads.upload_id
-- async_tasks.biz_id          → multipart_uploads.upload_id / objects.id
-- metering_daily.user_id      → users.id
-- metering_daily.bucket_id    → buckets.id
-- operation_logs.user_id      → users.id
-- operation_logs.bucket_id    → buckets.id
-- video_transcodes.user_id    → users.id
-- video_transcodes.bucket_id  → buckets.id
-- video_transcodes.object_id  → objects.id
-- video_transcode_profiles.transcode_id → video_transcodes.id
-- video_encrypt_keys.transcode_id       → video_transcodes.id
-- video_encrypt_keys.profile_id         → video_transcode_profiles.id

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
