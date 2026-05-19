# 视频转码 + HLS 播放加密实现计划

## 定位

本功能提供视频对象的转码、HLS 切片、HLS AES-128 播放加密和播放授权。

它不是对象存储静态加密，也不是 DRM：

- HLS 加密保护播放链路中的 m3u8、ts 和 key 获取流程。
- 原始对象仍受现有 AK/SK、Token、Policy 体系保护。
- 如果业务要求用户只能在线播放、不能下载原始 MP4，需要额外增加对象访问策略，例如禁止 `GetObject` 直读源视频，或增加 `video_play_only` 元数据。

## 技术栈

- 转码：ffmpeg
- 切片：HLS VOD，默认 10 秒一个 segment
- 加密：HLS AES-128，profile 级独立 key
- 任务队列：复用现有 `async_tasks`
- 存储：OSS Storage 扩展派生资产接口
- 播放端：hls.js

## 不实现

- HTTP Range：HLS 播放链路不依赖 Range；普通 `GetObject` 的 Range 支持单独设计。
- 对象级 SSE：本计划不做静态加密，后续如需要应作为通用对象能力实现。
- DRM license：不做 Widevine/FairPlay/PlayReady 这类 DRM 授权。
- 在线转封装：只处理上传完成后的异步转码，不做边上传边转码。

---

## 核心约束

1. `async_tasks` 当前没有 `payload` 字段，转码任务不能依赖 `task.payload`。
2. 每个转码 profile 对应一条 `video_transcode_profiles` 记录，async task 只保存 `profile_id`。
3. HLS 产物是派生资产，不进入普通 `objects` 列表，不复用 `PutObject/GetObject` 语义。
4. Storage I/O 必须在 DB 事务外执行。
5. ffmpeg 执行必须使用 `exec.CommandContext`，禁止 shell 字符串拼接。
6. 播放 token 必须绑定源对象、版本、transcode 和 key，不能只校验 action。
7. HLS playlist、segment、key server 都必须校验播放 token。

---

## 整体数据流

```text
PutObject(video.mp4)
    ↓
afterPutObject 判断视频类型
    ↓
创建 video_transcodes
    ↓
创建 video_transcode_profiles，每个 profile 一条记录
    ↓
为每个 profile 创建 async_task
    task_type = TRANSCODE
    biz_type  = video_profile
    biz_id    = profile_id
    ↓
Worker 消费 taskID
    ├── task.BizID -> profile_id
    ├── 查询 profile/transcode/source object
    ├── 下载源对象到临时目录
    ├── 为 profile 获取或生成 HLS AES key
    ├── 写 key.info
    ├── ffmpeg 转码 + 切片 + 加密
    ├── 上传到 staging asset 前缀
    ├── 发布到正式 asset 前缀
    └── 更新 profile 状态和派生资产大小
    ↓
前端播放
    ├── POST /api/v1/video/play-tokens 获取 token + play_url
    ├── hls.js 请求 master.m3u8，携带 X-Play-Token
    ├── hls.js 请求 profile m3u8 和 ts，携带 X-Play-Token
    └── hls.js 请求 key server，携带 X-Play-Token
```

---

## TASK 1 · 数据库设计

### video_transcodes

```sql
CREATE TABLE video_transcodes (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    bucket_id BIGINT NOT NULL,
    bucket_name VARCHAR(128) NOT NULL,
    object_id BIGINT UNSIGNED NOT NULL,
    object_key VARCHAR(1024) NOT NULL,
    object_key_hash CHAR(32) NOT NULL,
    version_id VARCHAR(64) NOT NULL,
    source_etag VARCHAR(64) NOT NULL,
    source_size BIGINT NOT NULL DEFAULT 0,

    status TINYINT NOT NULL DEFAULT 0 COMMENT '0=pending 1=processing 2=done 3=failed 4=deleted',
    duration_ms BIGINT NOT NULL DEFAULT 0,
    derived_size BIGINT NOT NULL DEFAULT 0,
    profile_count INT NOT NULL DEFAULT 0 COMMENT`码率`,
    done_profile_count INT NOT NULL DEFAULT 0 COMMENT`done_profile_count 每个 profile 完成时 +1`,
    last_error TEXT DEFAULT NULL,

    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    finished_at DATETIME(3) DEFAULT NULL,

    PRIMARY KEY (id),
    UNIQUE KEY uk_video_transcodes_object_version (object_id, version_id),
    KEY idx_video_transcodes_user_status (user_id, status),
    KEY idx_video_transcodes_bucket_object (bucket_id, object_key_hash)
);
```

说明：

- `version_id` 必须记录，避免对象覆盖后转错源文件。
- `source_etag/source_size` 用于校验源对象是否仍是创建任务时的版本。
- `derived_size` 用于派生资产计费和清理扣减。

### video_transcode_profiles

```sql
CREATE TABLE video_transcode_profiles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    transcode_id BIGINT UNSIGNED NOT NULL,
    profile VARCHAR(32) NOT NULL COMMENT '1080p/720p/480p/360p',

    status TINYINT NOT NULL DEFAULT 0 COMMENT '0=pending 1=processing 2=done 3=failed 4=deleted',
    video_bitrate VARCHAR(32) NOT NULL,
    audio_bitrate VARCHAR(32) NOT NULL,
    width INT NOT NULL DEFAULT 0,
    height INT NOT NULL DEFAULT 0,

    asset_prefix VARCHAR(512) NOT NULL DEFAULT '',
    playlist_key VARCHAR(512) NOT NULL DEFAULT '',
    size BIGINT NOT NULL DEFAULT 0,
    segment_count INT NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    last_error TEXT DEFAULT NULL,

    started_at DATETIME(3) DEFAULT NULL,
    finished_at DATETIME(3) DEFAULT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    UNIQUE KEY uk_video_profiles_transcode_profile (transcode_id, profile),
    KEY idx_video_profiles_status_updated (status, updated_at)
);
```

说明：

- `asset_prefix` 指向正式 HLS 产物前缀，例如 `_video/{transcode_id}/{profile}/`。
- `playlist_key` 指向 profile m3u8，例如 `_video/{transcode_id}/{profile}/index.m3u8`。
- master.m3u8 默认动态生成，不要求落盘；如后续缓存 master，只作为可重建缓存。

### video_encrypt_keys

```sql
CREATE TABLE video_encrypt_keys (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    transcode_id BIGINT UNSIGNED NOT NULL,
    profile_id BIGINT UNSIGNED NOT NULL,
    key_id VARCHAR(64) NOT NULL,
    encrypted_key VARBINARY(512) NOT NULL,
    algorithm VARCHAR(32) NOT NULL DEFAULT 'HLS-AES-128',
    key_version VARCHAR(64) NOT NULL DEFAULT '',
    kms_key_id VARCHAR(128) NOT NULL DEFAULT '',

    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    UNIQUE KEY uk_video_encrypt_keys_key_id (key_id),
    UNIQUE KEY uk_video_encrypt_keys_profile (profile_id)
);
```

说明：

- 每个 profile 独立 AES key，避免多个码率共用 key 和相同 media sequence IV。
- `encrypted_key` 入库前使用服务端主密钥加密，接口返回时解密成 16 字节 raw key。
- `key_version/kms_key_id` 预留给后续主密钥轮换或 KMS。
- ffmpeg `key.info` 默认不写第三行固定 IV，让 HLS 使用 media sequence 派生 IV。

### video_play_tokens

播放 token 可以继续使用 Redis，不强制建表。Redis value 必须包含：

```text
token
user_id
bucket_id
bucket_name
object_id
object_key
version_id
transcode_id
expires_at
action = PlayVideo
```

验收：

- [ ] `init.sql` 执行通过。
- [ ] GORM Gen 重新生成 model/query。
- [ ] 表索引覆盖 object/version 查询、profile 状态查询、key_id 查询。

---

## TASK 2 · 常量与配置

复用现有 async task 类型：

```go
TaskTypeTranscode = "TRANSCODE"
TaskBizTypeVideoProfile = "video_profile"
```

新增业务常量：

```go
TranscodeStatusPending    = 0
TranscodeStatusProcessing = 1
TranscodeStatusDone       = 2
TranscodeStatusFailed     = 3
TranscodeStatusDeleted    = 4

PlayVideoAction = "PlayVideo"
GetTranscodeStatusAction = "GetTranscodeStatus"

HLSAssetPrefix = "_video"
HLSEncryptionAlgorithm = "HLS-AES-128"
HLSSegmentDurationSeconds = 10
DefaultPlayTokenTTLSeconds = 14400
DefaultTranscodeMaxConcurrency = 1
```

默认 profile：

```text
1080p: height=1080 video=4000k audio=128k
720p:  height=720  video=2000k audio=128k
480p:  height=480  video=800k  audio=96k
360p:  height=360  video=400k  audio=64k
```

视频类型判断：

```text
Content-Type:
  video/mp4
  video/quicktime
  video/x-matroska
  video/x-msvideo

Extension:
  .mp4
  .mov
  .mkv
  .avi
```

验收：

- [x] `ValidAsyncTaskType` 已包含 `TaskTypeTranscode`。
- [x] 编译通过。
- [x] `IsVideoObject` 能识别视频/非视频；TASK 5 触发转码时必须复用该判断。

---

## TASK 3 · Storage 派生资产接口

HLS 产物不应写入普通 `objects` 表。Storage 层需要新增派生资产能力，Service 只依赖接口。

```go
type IVideoAssetStorage interface {
    PutAsset(ctx context.Context, bucket string, assetKey string, src io.Reader) (*storage.PutResult, error)
    GetAsset(ctx context.Context, bucket string, assetKey string) (io.ReadCloser, error)
    DeleteAsset(ctx context.Context, bucket string, assetKey string) error
    DeleteAssetPrefix(ctx context.Context, bucket string, prefix string) error
    MoveAssetPrefix(ctx context.Context, bucket string, srcPrefix string, dstPrefix string) error
}
```

本地存储路径建议：

```text
{baseDir}/{bucket}/_video/{transcode_id}/{profile}/index.m3u8
{baseDir}/{bucket}/_video/{transcode_id}/{profile}/seg_000001.ts
{baseDir}/{bucket}/_video/{transcode_id}/{profile}/staging/{task_id}/...
```

约束：

- 派生资产不出现在 `ListObjects`。
- 派生资产读取必须走 video 播放接口，不允许直接暴露物理路径。
- `DeleteAssetPrefix` 用于对象删除、版本清理、转码失败清理。
- `MoveAssetPrefix` 用于将 staging 前缀发布到正式前缀；本地实现用目录移动，S3/MinIO 实现需要内部完成 copy+delete。

验收：

- [x] 本地 Storage 支持写入、读取、删除单个 HLS asset。
- [x] 本地 Storage 支持按 prefix 删除 HLS asset。
- [x] 本地 Storage 支持 staging prefix 发布到正式 prefix。
- [x] Service 层不直接 import local storage 实现。

---

## TASK 4 · Repo 接口

```go
type IVideoRepo interface {
    WithTx(tx tx.Tx) IVideoRepo

    CreateTranscode(ctx context.Context, in *do.CreateVideoTranscode) (*do.VideoTranscodeDo, error)
    GetTranscodeByObjectVersion(ctx context.Context, objectID int64, versionID string) (*do.VideoTranscodeDo, error)
    GetTranscodeByID(ctx context.Context, transcodeID int64) (*do.VideoTranscodeDo, error)
    UpdateTranscode(ctx context.Context, transcodeID int64, in *do.UpdateVideoTranscode) error
    MarkTranscodeDeleted(ctx context.Context, transcodeID int64) error

    CreateProfiles(ctx context.Context, transcodeID int64, profiles []*do.CreateVideoProfile) ([]*do.VideoProfileDo, error)
    GetProfileByID(ctx context.Context, profileID int64) (*do.VideoProfileDo, error)
    ListProfiles(ctx context.Context, transcodeID int64) ([]*do.VideoProfileDo, error)
    ListDoneProfiles(ctx context.Context, transcodeID int64) ([]*do.VideoProfileDo, error)
    UpdateProfile(ctx context.Context, profileID int64, in *do.UpdateVideoProfile) error

    SaveEncryptKey(ctx context.Context, in *do.CreateVideoEncryptKey) error
    GetEncryptKeyByKeyID(ctx context.Context, keyID string) (*do.VideoEncryptKeyDo, error)
    GetEncryptKeyByProfileID(ctx context.Context, profileID int64) (*do.VideoEncryptKeyDo, error)
}
```

错误边界：

- Repo 只返回 repoerr 包装后的错误。
- Service 禁止检查 `gorm.ErrRecordNotFound`。
- 重复创建 transcode/profile/task 时按幂等返回已有记录。

验收：

- [x] Repo 单测覆盖重复创建、not found、状态更新和 DO 转换；DB 行为使用 sqlmock 覆盖。
- [x] `WithTx(tx tx.Tx)` 是接口第一项。
- [x] 新增 video DO/Repo 不让 Service 层依赖 `gorm.io/gorm`；现有 `service/do/object.go` 仍有历史 `gorm.DeletedAt`。

---

## TASK 5 · PutObject 触发转码

触发点：

- 普通 `PutObject` 成功提交 DB 后触发。
- `CompleteMultipartUpload` 创建对象版本成功后触发。
- 触发逻辑必须在对象事务提交后执行，避免事务内做 Redis 或 Storage I/O。

伪代码：

```text
afterPutObject(object):
  if not isVideoFile(object.content_type, object.object_key):
    return

  transcode = createOrGetTranscode(
    user_id,
    bucket_id,
    bucket_name,
    object_id,
    object_key,
    version_id,
    etag,
    size
  )

  profiles = createOrGetProfiles(transcode.id, DefaultProfiles)

  for profile in profiles:
    taskID = asyncRepo.CreateAsyncTask(
      user_id   = object.user_id,
      task_type = TRANSCODE,
      biz_type  = video_profile,
      biz_id    = profile.id,
      status    = PENDING,
      max_retry = 3
    )
    enqueueAsyncTask(taskID)
```

注意：

- 不写 `payload`，worker 通过 `profile_id` 反查上下文。
- Redis 入队失败由现有 pending scanner 兜底。
- 如果对象被覆盖，新版本会有新的 `version_id` 和新的 transcode。

验收：

- [ ] 上传视频后出现一条 `video_transcodes`。
- [ ] 默认出现 4 条 `video_transcode_profiles`。
- [ ] 出现 4 条 `async_tasks`，`task_type=TRANSCODE`，`biz_type=video_profile`。
- [ ] 重复触发不会创建重复 profile/task。

---

## TASK 6 · 转码 Worker

任务入口：

```text
handleTranscodeTask(task):
  if task.BizType != video_profile:
    fail task

  profileID = parseInt(task.BizID)
  profile = videoRepo.GetProfileByID(profileID)
  transcode = videoRepo.GetTranscodeByID(profile.TranscodeID)
  source = objectRepo.GetByIDAndVersion(transcode.ObjectID, transcode.VersionID)

  if source.etag != transcode.SourceEtag:
    fail task
```

执行流程：

```text
tmpDir = os.MkdirTemp("", "oss_video_{taskID}_")
defer cleanup tmpDir

mark profile processing

download source object -> tmpDir/input
getOrCreate profile AES key
write tmpDir/enc.key
write tmpDir/key.info

ffmpeg:
  -i tmpDir/input
  -hls_key_info_file tmpDir/key.info
  -hls_time 10
  -hls_playlist_type vod
  -hls_segment_filename tmpDir/out/seg_%06d.ts
  -vf scale=-2:{height}
  -b:v {videoBitrate}
  -b:a {audioBitrate}
  tmpDir/out/index.m3u8

upload tmpDir/out/* -> staging prefix
copy/upload staging -> final prefix
delete staging prefix

update profile done(size, segment_count, duration, playlist_key)
update transcode aggregate(done_profile_count, derived_size, status)
```

key.info 内容：

```text
{base_url}/api/v1/video/keys/{key_id}
{tmpDir}/enc.key
```

说明：

- 不写第三行固定 IV，避免所有 segment 共用同一 IV。
- 每个 profile 独立 key，减少 IV 重复风险。
- key URI 不包含 token，token 通过 `X-Play-Token` header 传递。

ffmpeg 执行规范：

- 使用 `exec.CommandContext(ctx, "ffmpeg", args...)`。
- 不通过 shell 拼接命令。
- stdout/stderr 最多截断保存 4KB 到 `last_error`。
- 任务取消或 Redis 执行锁丢失时，context 取消 ffmpeg。
- worker 需要全局并发限制，例如 `video.transcode.max_concurrency`。

失败处理：

- profile 标记 failed，写入 `last_error`。
- async task 走现有 `FailAsyncTask` 重试。
- 清理本次 staging prefix。
- 不删除已发布且 DB 指向的旧正式 prefix。

验收：

- [ ] ffmpeg 不存在时任务失败且错误可见。
- [ ] 转码成功后 profile 状态为 done。
- [ ] 转码失败后 staging 目录被清理。
- [ ] 重试不会破坏已完成 profile。
- [ ] worker 并发数可配置。

---

## TASK 7 · 播放 playlist 和 segment

播放 URL：

```text
GET /api/v1/video/hls/:transcode_id/master.m3u8
GET /api/v1/video/hls/:transcode_id/:profile/index.m3u8
GET /api/v1/video/hls/:transcode_id/:profile/:segment
```

认证方式：

```text
Header:
  X-Play-Token: {token}
```

query token 仅作为兼容方案：

```text
?token=xxx
```

默认推荐 header，避免 token 出现在日志、Referer 和持久化 playlist 中。

校验逻辑：

```text
validatePlayToken(token):
  action == PlayVideo
  transcode_id matches route
  object_id/version_id matches transcode
  token not expired
```

master.m3u8 默认动态生成：

```text
list done profiles
sort by height desc
render #EXT-X-STREAM-INF
variant URL = /api/v1/video/hls/{transcode_id}/{profile}/index.m3u8
```

profile m3u8 返回前需要确保 key URI 指向：

```text
/api/v1/video/keys/{key_id}
```

segment 读取：

```text
profile -> asset_prefix -> segment asset key
assetStorage.GetAsset(bucket, assetKey)
Content-Type: video/MP2T
```

验收：

- [ ] 无 token 访问 master/profile/segment 返回 401。
- [ ] token 绑定 transcode 不匹配返回 403。
- [ ] 已完成 profile 会出现在 master.m3u8。
- [ ] 未完成 profile 不出现在 master.m3u8。
- [ ] segment 不暴露本地物理路径。

---

## TASK 8 · Key Server

```text
GET /api/v1/video/keys/:key_id
Header: X-Play-Token: {token}
```

流程：

```text
1. 读取 token，优先 X-Play-Token，兼容 query token
2. 校验 token 基本有效性（格式、签名、是否过期）  ← 先校验
3. 查询 video_encrypt_keys by key_id
4. 查询 profile/transcode
4.5. 查询 transcode.status
     if transcode.status == deleted → 403
5. 解密 encrypted_key
6. 返回 200 application/octet-stream，body 为 16 字节 raw AES key
```

禁止：

- 禁止只校验 `action=PlayVideo`。
- 禁止缓存 raw AES key 到客户端可复用位置。
- 禁止把 raw AES key 写入日志。

验收：

- [ ] 有效 token 返回 16 字节 binary。
- [ ] token 过期返回 401。
- [ ] token 和 key 不属于同一 transcode 返回 403。
- [ ] key 不存在返回 404。
- [ ] 日志不包含 raw key。

---

## TASK 9 · 播放 Token

```text
POST /api/v1/video/play-tokens
需要 AK/SK 或已有用户认证
```

请求：

```json
{
  "bucket_name": "my-bucket",
  "object_key": "movie.mp4",
  "version_id": "",
  "expires_in": 14400
}
```

流程：

```text
1. 校验用户对源对象有 PlayVideo 权限。
2. 查询 object 和 transcode。
3. 如果 transcode 不存在，返回未转码状态。
4. 如果没有任何 done profile，返回 processing 状态。
5. 创建 Redis play token，绑定 user/bucket/object/version/transcode。
6. 返回 token、play_url、expires_at、status。
```

响应：

```json
{
  "token": "xxx",
  "play_url": "/api/v1/video/hls/123/master.m3u8",
  "expires_at": 1710000000,
  "status": 2,
  "profiles": ["720p", "480p"]
}
```

hls.js 使用方式：

```js
function getPlayToken() {
  return token
}

class TokenKeyLoader extends Hls.DefaultConfig.loader {
  load(context, config, callbacks) {
    context.headers = context.headers || {}
    context.headers["X-Play-Token"] = getPlayToken()
    super.load(context, config, callbacks)
  }
}

const hls = new Hls({
  keyLoader: TokenKeyLoader,
  xhrSetup: function (xhr) {
    xhr.setRequestHeader("X-Play-Token", getPlayToken())
  }
})
hls.loadSource(playUrl)
hls.attachMedia(videoElement)
```

说明：

- `xhrSetup` 覆盖 playlist 和 segment 请求。
- key 请求需要单独配置 `keyLoader` 注入 `X-Play-Token`。
- 联调时如果 master/profile m3u8 正常加载但画面黑屏或花屏，优先检查 key 请求是否携带 token。

验收：

- [ ] 未认证用户不能创建 play token。
- [ ] 无对象权限不能创建 play token。
- [ ] token 绑定 version_id。
- [ ] hls.js playlist、segment、key 请求都能通过 header 携带 token。

---

## TASK 10 · 转码状态查询

```text
GET /api/v1/buckets/:bucket_name/objects/:object_key/transcode?version_id=xxx
需要 AK/SK 或已有用户认证
```

响应：

```json
{
  "object_key": "movie.mp4",
  "version_id": "v1",
  "status": 1,
  "duration_ms": 120000,
  "derived_size": 104857600,
  "profiles": [
    {
      "profile": "720p",
      "status": 2,
      "width": 1280,
      "height": 720,
      "size": 52428800,
      "segment_count": 12,
      "last_error": ""
    }
  ]
}
```

验收：

- [ ] 查询返回 transcode 主状态。
- [ ] 查询返回每个 profile 的状态和错误。
- [ ] 未触发转码的视频返回明确状态。

---

## TASK 11 · 路由注册

公开但需要播放 token 的路由：

```text
GET /api/v1/video/hls/:transcode_id/master.m3u8
GET /api/v1/video/hls/:transcode_id/:profile/index.m3u8
GET /api/v1/video/hls/:transcode_id/:profile/:segment
GET /api/v1/video/keys/:key_id
```

需要用户认证的路由：

```text
POST /api/v1/video/play-tokens
GET  /api/v1/buckets/:bucket_name/objects/:object_key/transcode
```

权限动作：

```text
PlayVideo
GetTranscodeStatus
```

验收：

- [ ] 路由启动无冲突。
- [ ] HLS 路由不挂 AK/SK，但必须校验播放 token。
- [ ] 管理/查询路由必须校验用户身份。

---

## TASK 12 · 删除、覆盖与版本清理

对象删除或版本 purge 时：

```text
1. 查询 object_id/version_id 对应 transcode。
2. 标记 video_transcodes.status=deleted。
3. 标记 profiles.status=deleted。
4. 删除 asset prefix: _video/{transcode_id}/。
5. 删除或失效 video_encrypt_keys。
6. 扣减 derived_size 对应的 bucket/user storage_used。
```

对象覆盖时：

- 如果 bucket 未开启版本，旧版本对应 HLS 产物应随旧对象清理。
- 如果 bucket 开启版本，旧版本 HLS 产物保留，按 version_id 播放。

验收：

- [ ] 覆盖视频不会误用旧版本 HLS。
- [ ] 删除对象后 play token 不再可用。
- [ ] 删除对象后 HLS 派生资产被清理。
- [ ] storage_used 正确扣减派生资产大小。

---

## TASK 13 · 计费与指标

派生资产默认计入存储用量：

```text
derived_size = sum(profile.size)
bucket.storage_size += profile.size
user.storage_used += profile.size
```

上传转码产物时不计入用户上传流量；播放 segment 时计入下载流量。

指标建议：

```text
video_transcode_total{status, profile}
video_transcode_duration_seconds{profile}
video_transcode_derived_bytes{profile}
video_play_token_total{result}
video_key_request_total{result}
video_segment_request_bytes
```

日志要求：

- 转码任务日志包含 `task_id/profile_id/transcode_id/object_id/version_id`。
- ffmpeg stderr 截断记录。
- key server 日志不包含 raw key。

验收：

- [ ] 转码成功后 storage_used 增加派生资产大小。
- [ ] 删除或 purge 后 storage_used 扣减。
- [ ] 下载 segment 计入 download flow。

---

## TASK 14 · 端到端联调

```bash
# 1. 上传视频
PUT /api/v1/buckets/my-bucket/objects/movie.mp4

# 2. 查询转码状态
GET /api/v1/buckets/my-bucket/objects/movie.mp4/transcode

# 3. 获取播放 token
POST /api/v1/video/play-tokens
{
  "bucket_name": "my-bucket",
  "object_key": "movie.mp4"
}

# 4. hls.js 播放
GET /api/v1/video/hls/{transcode_id}/master.m3u8
X-Play-Token: {token}
```

前端联调必须同时配置 `xhrSetup` 和自定义 `keyLoader`：

```js
function getPlayToken() {
  return token
}

class TokenKeyLoader extends Hls.DefaultConfig.loader {
  load(context, config, callbacks) {
    context.headers = context.headers || {}
    context.headers["X-Play-Token"] = getPlayToken()
    super.load(context, config, callbacks)
  }
}

const hls = new Hls({
  keyLoader: TokenKeyLoader,
  xhrSetup(xhr) {
    xhr.setRequestHeader("X-Play-Token", getPlayToken())
  }
})

hls.loadSource(playUrl)
hls.attachMedia(videoElement)
```

高优先级联调检查：

- master.m3u8 和 profile m3u8 请求必须携带 `X-Play-Token`。
- `.ts` segment 请求必须携带 `X-Play-Token`。
- `/api/v1/video/keys/:key_id` 请求必须携带 `X-Play-Token`。
- 如果 m3u8 能加载但画面黑屏、花屏或解密失败，先检查 key 请求是否遗漏 token。

验收：

- [ ] 视频正常播放。
- [ ] 进度条可拖动。
- [ ] 无 token 无法获取 m3u8、ts、key。
- [ ] key 请求遗漏 token 时返回 401，前端能定位到 keyLoader 配置问题。
- [ ] 错误 token 无法跨视频获取 key。
- [ ] token 过期后无法继续请求 key。
- [ ] 直接访问物理 `_video` 路径不可用。
- [ ] 原始 MP4 是否允许下载符合访问策略。

---

## 实现顺序

1. 数据库表和 GORM Gen。
2. Video Repo、DO、DTO。
3. Storage 派生资产接口。
4. Play token Redis 能力。
5. PutObject/CompleteMultipartUpload 触发转码任务。
6. 转码 worker。
7. HLS playlist、segment、key server 路由。
8. 删除、覆盖、版本清理。
9. 计费、日志、指标。
10. 端到端测试。

---

## 风险点

| 风险 | 处理 |
|---|---|
| async task 没有 payload | `biz_id=profile_id`，worker 反查 DB |
| query token 泄漏 | 默认使用 `X-Play-Token` header，query 仅兼容短 TTL |
| hls.js key 请求未注入 token | 自定义 `keyLoader`，`xhrSetup` 只作为 playlist/segment header 注入 |
| key 越权 | key_id 必须反查 profile/transcode 并和 token 绑定值比较 |
| 对象覆盖后转错源 | transcode 绑定 `object_id/version_id/source_etag` |
| ffmpeg 资源占用高 | worker 加全局并发限制和 context 超时 |
| 重试产生脏文件 | staging prefix + 失败清理 |
| master.m3u8 并发覆盖 | master 默认动态生成，不落盘为主 |
| 派生资产孤儿文件 | 删除、覆盖、purge 路径统一清理 `_video/{transcode_id}/` |
| IV 复用 | profile 独立 key，不写固定 IV |

---

## 依赖

| 依赖 | 用途 |
|---|---|
| ffmpeg | 转码、HLS 切片、AES-128 加密 |
| github.com/google/uuid | 生成 `key_id` |
| hls.js | 浏览器播放 HLS 并设置 `X-Play-Token` |
