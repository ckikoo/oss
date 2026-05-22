# Video Cache 设计方案

## 0. 评估结论

视频播放链路适合引入 `本地 LRU + Redis + singleflight` 缓存，但落地时必须把一致性边界写清楚。数据库仍然是 source of truth，Redis 只是跨进程缓存，本地 LRU 只是进程内短 TTL 缓存。

本方案按以下原则修订：

- 事务内 repo 不读写缓存，避免未提交数据进入 Redis 或本地缓存。
- 写操作成功后只做删除式失效，不做写回式更新。
- 缓存失效通过同步 after-commit hook 执行，禁止事务提交前删缓存。
- 对播放状态、profile 列表、加密 key 这类高风险缓存，提交后立即删一次，并按需延迟双删兜底并发旧读回填。
- `Transcode` 的 by-id 和 by-object-version 两个 key 必须成对回填、成对失效。
- 批量删除 profile / encrypt key 前要先收集被影响的单体 key，不能只删 list key。
- 缓存读写失败不影响主流程，只记录日志并回源 DB。
- 第一阶段不做 negative cache，避免“先查不到，随后创建成功”导致短时间不可见。

---

## 1. 当前代码约束

现有缓存基础设施已经具备：

- `utils/cache.Manager` 提供本地 LRU、Redis Stream 失效广播和本地订阅。
- `cache.IManager.Get` 返回 `*cache.Entry`，业务对象在 `entry.Data`。
- 本地缓存默认 TTL 是 30 秒，Redis TTL 由业务 cache key 常量控制。
- 当前跨实例失效使用 Redis Stream，不是 Redis Pub/Sub。它是缓存一致性的加速通道，不是 correctness source。
- `tx.ITxManager` 目前只有 `RunInTx`，需要补充 after-commit hook 才能让事务内写操作自动登记提交后失效。

因此视频 repo 改造要和现有 bucket/cors/event/object repo 的模式保持一致：

```go
if entry, ok := r.cacheManager.Get(cacheKey); ok {
	if value, ok := entry.Data.(*do.VideoTranscodeDo); ok {
		return value, nil
	}
	r.cacheManager.Remove(cacheKey)
}
```

`singleflight.Group` 应使用指针，并在 `WithTx` repo 中复用同一个实例，避免复制含锁对象：

```go
g *singleflight.Group
```

---

## 2. Cache Key 定义

在 `consts/cache_keys.go` 增加：

```go
const (
	CacheKeyVideoTranscodeByID     = "oss:video:transcode:id:%d"
	CacheKeyVideoTranscodeByObjVer = "oss:video:transcode:obj:%d:%s"

	CacheKeyVideoProfilesByTranscode = "oss:video:profiles:%d"
	CacheKeyVideoDoneProfilesByTC    = "oss:video:profiles:done:%d"
	CacheKeyVideoProfileByID         = "oss:video:profile:id:%d"

	CacheKeyVideoEncryptKeyByKeyID     = "oss:video:enckey:keyid:%s"
	CacheKeyVideoEncryptKeyByProfileID = "oss:video:enckey:profile:%d"

	CacheTTLVideoTranscode  = 600  // 10 分钟
	CacheTTLVideoProfile    = 300  // 5 分钟
	CacheTTLVideoEncryptKey = 3600 // 1 小时
)
```

辅助函数：

```go
func VideoTranscodeCacheKey(transcodeID int64) string {
	return fmt.Sprintf(CacheKeyVideoTranscodeByID, transcodeID)
}

func VideoTranscodeByObjectVersionCacheKey(objectID int64, versionID string) string {
	return fmt.Sprintf(CacheKeyVideoTranscodeByObjVer, objectID, versionID)
}

func VideoProfilesCacheKey(transcodeID int64) string {
	return fmt.Sprintf(CacheKeyVideoProfilesByTranscode, transcodeID)
}

func VideoDoneProfilesCacheKey(transcodeID int64) string {
	return fmt.Sprintf(CacheKeyVideoDoneProfilesByTC, transcodeID)
}

func VideoProfileCacheKey(profileID int64) string {
	return fmt.Sprintf(CacheKeyVideoProfileByID, profileID)
}

func VideoEncryptKeyByKeyIDCacheKey(keyID string) string {
	return fmt.Sprintf(CacheKeyVideoEncryptKeyByKeyID, keyID)
}

func VideoEncryptKeyByProfileIDCacheKey(profileID int64) string {
	return fmt.Sprintf(CacheKeyVideoEncryptKeyByProfileID, profileID)
}
```

---

## 3. 缓存范围

| 数据 | 本地 LRU | Redis | 说明 |
|---|---:|---:|---|
| Transcode by id | 是 | 是 | HLS 播放、状态校验高频读取 |
| Transcode by object/version | 是 | 是 | 创建播放 token、状态查询高频读取 |
| Profile by id | 是 | 是 | worker 和 key 校验读取 |
| Profile list | 是 | 是 | profile 数量少，按 transcodeID 缓存可控 |
| Done profile list | 是 | 是 | 播放链路高频读取，按 transcodeID 缓存 |
| Encrypt key by keyID | 是 | 是 | HLS key 请求高频读取 |
| Encrypt key by profileID | 是 | 是 | profile playlist 和转码任务读取 |

不缓存：

- `ListProfiles` / `ListDoneProfiles` 的空值 negative cache。
- 事务内查询结果。
- 需要强实时的临时聚合结果。

---

## 4. Repo 改造

### 4.1 构造函数

当前 `NewVideoRepo(db *gorm.DB)` 需要改为接收 `adaptor.IAdaptor`，否则拿不到 Redis 和本地 cache manager。

```go
type VideoRepo struct {
	db           *gorm.DB
	q            *query.Query
	rds          *redis.Client
	cacheManager cache.IManager
	g            *singleflight.Group
	cacheEnabled bool
}

func NewVideoRepo(a adaptor.IAdaptor) video.IVideoRepo {
	db := a.GetGORM()
	return &VideoRepo{
		db:           db,
		q:            query.Use(db),
		rds:          a.GetRedis(),
		cacheManager: a.GetCache(),
		g:            &singleflight.Group{},
		cacheEnabled: true,
	}
}
```

调用点需要同步从：

```go
gormVideo.NewVideoRepo(adaptor.GetGORM())
```

改为：

```go
gormVideo.NewVideoRepo(adaptor)
```

涉及 `scheduler.go`、`processor.go`、`playback.go`、`clean.go`。

### 4.2 事务 repo

`WithTx` 返回的 repo 必须禁用缓存读写。事务中的 DB 读写仍走 tx，但不访问 Redis、本地 LRU，也不发布失效消息。

```go
func (r *VideoRepo) WithTx(tx tx.Tx) video.IVideoRepo {
	db := tx.(*gorm.DB)
	return &VideoRepo{
		db:           db,
		q:            query.Use(db),
		rds:          r.rds,
		cacheManager: r.cacheManager,
		g:            r.g,
		cacheEnabled: false,
	}
}
```

事务内写操作涉及的 cache key 不在事务内删除，而是通过 `tx.AfterCommit(ctx, fn)` 登记提交后失效。没有事务上下文时，写操作成功后直接同步失效。

---

## 5. 读取流程

所有缓存读取采用三层模式：

```text
本地 LRU -> Redis -> singleflight -> DB
```

缓存 miss 回源 DB 后写入 Redis 和本地 LRU。缓存命中但类型断言失败时删除本地 key 并继续回源。

Redis 序列化第一阶段建议使用 `encoding/json`，和现有 bucket/cors/event/object repo 保持一致。但实现时应预留 codec 接口，避免未来从 `encoding/json` 切到 Sonic 时散改每个 repo。

```go
直接开json 包 util/json ...
```

repo 不直接依赖具体 JSON 库，只依赖 codec helper。后续若基准测试证明 Sonic 收益明确，可以替换 codec 实现。

### 5.1 Transcode 双 key 回填

`GetTranscodeByID` 回源 DB 成功后，应同时写入：

- `VideoTranscodeCacheKey(transcode.ID)`
- `VideoTranscodeByObjectVersionCacheKey(transcode.ObjectID, transcode.VersionID)`

`GetTranscodeByObjectVersion` 也一样。这样无论从哪个入口回源，都能让两个入口保持一致。

### 5.2 Profile list

`ListProfiles` 和 `ListDoneProfiles` 可以缓存，但失效点必须覆盖：

- `CreateProfiles`
- `UpdateProfile`
- `MarkProfilesDeleted`
- `DeleteEncryptKeysByTranscodeID` 关联删除时涉及的 profile 状态判断

list cache 中只存 `[]*do.VideoProfileDo`，不缓存空结果的“未找到”语义。

list cache key 统一只按 `transcodeID` 建模：

```text
oss:video:profiles:{transcodeID}
oss:video:profiles:done:{transcodeID}
```

不要把 profile 状态、profile 名称或分页参数放入 key。当前接口没有分页，统一 `transcodeID` 能保证所有 profile 变更都只需要删除两个 list key。

所有会改变 profile 集合或可见状态的写操作，必须先收集 `profileIDs`，再统一删除：

- `ProfileByID(profileID...)`
- `ProfilesByTranscode(transcodeID)`
- `DoneProfilesByTranscode(transcodeID)`

禁止在各个写方法里手写一份 key 列表，应封装成 `invalidateProfilesByTranscodeAfterCommit(ctx, transcodeID, profileIDs...)`。

### 5.3 Singleflight 与旧值回填

`singleflight` 只合并同进程同 key 的 DB 回源，不能阻止另一个 goroutine 在事务提交前读到旧 DB 并在提交后写回缓存。因此：

- singleflight key 必须等于 cache key，避免不同入口重复回源。
- DB 回源完成后，set cache 前可校验 `ctx.Err()`；请求已取消时不写缓存。
- 对高风险 key，仍依赖 after-commit 删除和延迟双删兜住旧值回填。
- 事务内读禁用 singleflight 和缓存写入，避免未提交数据进入共享缓存。

---

## 6. 失效规则

失效只在 DB commit 成功后执行。禁止事务提交前删缓存，否则可能出现以下长时间 stale：

```text
T1 更新 DB 未提交 -> T1 先删缓存 -> T2 miss 后读到旧 DB -> T2 把旧值写回缓存 -> T1 commit
```

提交后同步删除缓存仍然存在一个很短的旧数据窗口：

```text
commit 成功 -> after-commit 删除缓存完成
```

这个窗口通常是毫秒级，可以接受。实现上 after-commit 必须同步执行，不起 goroutine，`RunInTx` 应在 hook 执行完成后才返回。

基础失效顺序：

```text
DB 写成功 -> Redis Del -> 本地 Remove -> Redis Stream Publish
```

事务内写操作不立即失效，只登记 after-commit hook。非事务写操作在 DB 写成功后直接同步失效。

高风险缓存采用延迟双删：

```text
commit -> 立即删除一次 -> 100~500ms 后再删除一次
```

延迟双删用于兜住极端并发：读请求在 commit 前 cache miss 并读取旧 DB，commit 后第一次删除已经完成，它随后又把旧值写回缓存。第二次删除可以清理这类旧值回填。

第一阶段建议启用延迟双删的缓存：

- transcode by id / by object-version
- profile by id
- profiles list / done profiles list
- encrypt key by keyID / by profileID

延迟双删失败只记录 warn，不影响业务返回。

延迟间隔必须做成可调参数，不要写死在业务代码里：

```go
type CacheInvalidationConfig struct {
	DelayedDoubleDeleteEnabled bool
	DelayedDoubleDeleteDelay   time.Duration // 默认 200ms，建议范围 100~500ms
	InvalidateBatchSize        int           // 默认 500
}
```

默认值先取 `200ms`。最终值应根据高并发压测、Redis RTT、DB 提交延迟和缓存回填耗时调整。低风险缓存可以关闭延迟双删，避免无意义的重复 Redis 操作。

跨实例失效使用 Redis Stream 广播，本质是 best-effort：

- Redis Stream 消息丢失或实例长时间离线时，依赖 Redis TTL、本地 TTL 和延迟双删兜底。
- 当前实现每个实例一个 consumer group，实例重启期间可能错过旧消息，不能把 Stream 当强一致机制。
- 如果未来需要更强的失效投递保证，应考虑专门 MQ/outbox；Redis Keyspace Notification 可作为观测或辅助，不建议作为唯一失效来源。

批量失效需要减少 Redis RTT：

- `DEL` 使用多 key 一次删除，key 数量过大时按 `InvalidateBatchSize` 分片。
- Redis Stream Publish 一次携带一批 keys，避免逐 key 发布。
- 删除大量 encrypt/profile key 时，先批量查询 key refs，再批量删除。
- 第一阶段不需要 Lua；如果后续还要同时维护索引 key，可再评估 Lua 脚本。

### 6.1 写操作映射

| 写操作 | 失效 key | 备注 |
|---|---|---|
| `CreateTranscode` | 无 | 不做 negative cache，创建新数据无需删旧 key |
| `UpdateTranscode` | transcode by id + transcode by object/version | 更新前或更新后必须拿到 `object_id/version_id` |
| `MarkTranscodeDeleted` | transcode by id + transcode by object/version | 复用 `UpdateTranscode` 失效逻辑 |
| `CreateProfiles` | profiles list + done profiles list | 防止已有空列表或旧列表缓存 |
| `UpdateProfile` | profile by id + profiles list + done profiles list | 需先查 profile 所属 `transcode_id` |
| `MarkProfilesDeleted` | profiles list + done profiles list + 每个 profile by id | 批量更新前先收集 profile IDs |
| `SaveEncryptKey` | encrypt key by profileID + encrypt key by keyID | 成功写入后失效即可 |
| `DeleteEncryptKeysByTranscodeID` | 每个 encrypt key by profileID + by keyID | 删除前先查 keyID/profileID 列表 |

### 6.2 Transcode 失效 helper

```go
func (r *VideoRepo) invalidateTranscodeCache(ctx context.Context, transcode *do.VideoTranscodeDo) {
	if transcode == nil {
		return
	}
	keys := []string{
		consts.VideoTranscodeCacheKey(transcode.ID),
		consts.VideoTranscodeByObjectVersionCacheKey(transcode.ObjectID, transcode.VersionID),
	}
	r.invalidateKeys(ctx, keys...)
}
```

`UpdateTranscode` 只有 `transcodeID`，所以不能只拼 by-id key。实现时需要直接从 DB 查询 `id/object_id/version_id`，并绕过缓存。

### 6.3 Profile 失效 helper

```go
func (r *VideoRepo) invalidateProfilesByTranscodeAfterCommit(ctx context.Context, transcodeID int64, profileIDs ...int64) {
	keys := []string{
		consts.VideoProfilesCacheKey(transcodeID),
		consts.VideoDoneProfilesCacheKey(transcodeID),
	}
	for _, profileID := range profileIDs {
		if profileID > 0 {
			keys = append(keys, consts.VideoProfileCacheKey(profileID))
		}
	}
	r.invalidateKeysAfterCommit(ctx, keys...)
}
```

所有 profile 变更必须统一走 `invalidateProfilesByTranscodeAfterCommit`，不要在 `CreateProfiles`、`UpdateProfile`、`MarkProfilesDeleted` 中分别手写 list key。这样 list cache 的 key 固定为 `transcodeID`，失效路径也固定为一个函数。

`UpdateProfile` 需要先查出该 profile 所属 `transcode_id`，再调用统一 helper。`CreateProfiles` 创建成功后，把返回的 profile IDs 传给统一 helper。`MarkProfilesDeleted` 需要批量查出该 transcode 下所有 profile id：

```text
SELECT id FROM video_transcode_profiles WHERE transcode_id = ?
```

然后调用统一 helper 删除 list key 和每个 `ProfileByID` key。

### 6.4 Encrypt key 失效 helper

```go
func (r *VideoRepo) invalidateEncryptKeyCache(ctx context.Context, keyID string, profileID int64) {
	keys := []string{
		consts.VideoEncryptKeyByKeyIDCacheKey(keyID),
		consts.VideoEncryptKeyByProfileIDCacheKey(profileID),
	}
	r.invalidateKeys(ctx, keys...)
}
```

`DeleteEncryptKeysByTranscodeID` 必须先查再删：

```text
SELECT key_id, profile_id FROM video_encrypt_keys WHERE transcode_id = ?
DELETE FROM video_encrypt_keys WHERE transcode_id = ?
invalidate collected keys after success
```

失效时不要逐条调用 Redis。应把查出的 `(key_id, profile_id)` 转为 key 列表后统一批量删除：

```text
DEL oss:video:enckey:keyid:{keyID...} oss:video:enckey:profile:{profileID...}
XADD cache invalidation stream {keys: [...]}
```

如果 key 数量超过批量上限，则按配置分片。每个分片仍应保持“Redis Del -> 本地 Remove -> Stream Publish”的顺序。

---

## 7. After-Commit Hook

### 7.1 Tx 接口补充

在 `tx` 包中增加 after-commit 注册能力。hook 只用于缓存失效、消息发布这类副作用，不承载核心业务正确性。

```go
type AfterCommitFunc func(context.Context)

func AfterCommit(ctx context.Context, fn AfterCommitFunc) bool {
	state, ok := ctx.Value(afterCommitKey{}).(*afterCommitState)
	if !ok {
		return false
	}
	state.add(fn)
	return true
}
```

`RunInTx` 创建带 hook 队列的 `txCtx`。事务提交成功后，同步执行 hook；事务失败或 panic 回滚时不执行。

```go
func (t *GormTxManager) RunInTx(ctx context.Context, fn func(context.Context, Tx) error) error {
	state := &afterCommitState{}
	txCtx := context.WithValue(ctx, afterCommitKey{}, state)

	err := t.db.WithContext(txCtx).Transaction(func(gormTx *gorm.DB) error {
		return fn(txCtx, gormTx)
	})
	if err != nil {
		return err
	}

	state.run(ctx)
	return nil
}
```

如果 hook 执行失败，只记录日志。DB 已提交，不能再通过 hook 错误回滚事务。

### 7.2 Repo 登记失效

repo 写操作在事务上下文中优先登记 after-commit：

```go
func (r *VideoRepo) invalidateKeysAfterCommit(ctx context.Context, keys ...string) {
	if len(keys) == 0 {
		return
	}
	if tx.AfterCommit(ctx, func(runCtx context.Context) {
		r.invalidateKeysWithDelayedDoubleDelete(runCtx, keys...)
	}) {
		return
	}
	r.invalidateKeysWithDelayedDoubleDelete(ctx, keys...)
}
```

`WithTx` repo 仍然禁用缓存读和缓存写，但允许写操作登记 after-commit 失效。也就是说：

- 事务内 `Get/List` 不读缓存、不写缓存。
- 事务内 `Update/Delete/Create` 不立即删缓存，只登记 after-commit。
- 非事务写操作直接执行同步失效。

### 7.3 `Processor.completeProfile`

事务内会执行：

- `UpdateProfile`
- `ListProfiles`
- `UpdateTranscode`
- bucket/user/metering 统计更新

事务内 `videoTx.ListProfiles` 必须查 DB，不走缓存。相关写操作应登记 after-commit，提交后失效：

- 当前 profile by id
- transcode 的 profiles list
- transcode 的 done profiles list
- transcode by id
- transcode by object/version

### 7.4 `CleanupService.MarkDeletedInTx`

事务内会执行：

- `MarkTranscodeDeleted`
- `MarkProfilesDeleted`
- `DeleteEncryptKeysByTranscodeID`

相关写操作应登记 after-commit，外层对象删除事务提交后失效：

- transcode by id
- transcode by object/version
- profiles list
- done profiles list
- 所有 profile by id
- 所有 encrypt key by keyID/profileID

如果 repo 内部无法在删除前拿到完整 profile/key 列表，需要在 `ObjectVersionCleanup` plan 中补充 `ProfileIDs`、`EncryptKeyIDs` 或 `EncryptKeyRefs`。

### 7.5 `Scheduler.ScheduleTranscode`

`CreateTranscode` 和 `CreateProfiles` 在事务内创建新数据。`CreateTranscode` 不需要失效旧 key；`CreateProfiles` 必须登记 profile list / done profile list 的 after-commit 失效。新建 transcode 通常没有旧列表缓存，但补档位、重试创建或未来扩展时可能存在旧列表，统一删除成本低且更稳。

---

## 8. 缓存失败处理

缓存是优化，不是正确性来源：

- Redis `Get` 失败：忽略，继续 DB。
- Redis `Set` 失败：记录 warn，不返回业务错误。
- Redis `Del` / `Publish` 失败：记录 warn，不回滚已提交 DB。
- JSON 反序列化失败：删除本地 key，忽略 Redis 值，继续 DB。
- 本地缓存类型断言失败：删除本地 key，继续 DB。

---

## 9. 实施路线

| 阶段 | 内容 | 优先级 |
|---|---|---|
| Phase 1 | 增加 video cache key 常量和 helper | P0 |
| Phase 2 | 给 `tx.ITxManager` 增加同步 after-commit hook | P0 |
| Phase 3 | `NewVideoRepo` 改为接收 `adaptor.IAdaptor`，同步所有调用点 | P0 |
| Phase 4 | 增加 repo 缓存字段，`WithTx` 禁用缓存读写并支持登记 after-commit 失效 | P0 |
| Phase 5 | 实现 `GetTranscodeByID` / `GetTranscodeByObjectVersion` 双 key 缓存 | P1 |
| Phase 6 | 实现 profile list、profile by id、encrypt key 缓存 | P1 |
| Phase 7 | 实现写操作 after-commit 失效和必要位置延迟双删 | P1 |
| Phase 8 | 增加单元测试和集成测试 | P2 |

---

## 10. 测试清单

- `GetTranscodeByID` 回源后能同时命中 by-id 和 by-object-version key。
- `GetTranscodeByObjectVersion` 回源后能同时命中两个 key。
- `UpdateTranscode` 后两个 transcode key 都失效。
- `UpdateProfile` 后 profile 单体、profiles list、done profiles list 都失效。
- `MarkProfilesDeleted` 后 list key 和所有 profile by-id key 都失效。
- `DeleteEncryptKeysByTranscodeID` 后所有 encrypt key by-keyID/by-profileID 都失效。
- `WithTx` 下读取不命中缓存，写入不污染缓存。
- `RunInTx` 提交成功后同步执行 after-commit hook。
- `RunInTx` 回滚后不执行 after-commit hook。
- `completeProfile` 事务回滚后不会把未提交 profile/transcode 写入缓存。
- 并发旧读在第一次删除后回填旧缓存时，延迟双删能清掉旧值。
- `ListProfiles` / `ListDoneProfiles` 只按 `transcodeID` 建 key，任何 profile 变更都删除两个 list key。
- 批量删除 encrypt key 时 Redis `DEL` 和 Stream Publish 都按批处理执行，不逐 key 往返。
- singleflight 场景下，事务外并发旧读回填缓存后仍能被延迟双删清理。
- Redis 不可用时视频查询仍能从 DB 返回。
- 缓存中存在错误类型或坏 JSON 时能回源并自愈。

---

## 11. 后续优化

- 对高频 HLS key 请求单独统计缓存命中率。
- 基于基准测试决定是否把视频缓存序列化从 `encoding/json` 切到 Sonic。
- 根据线上命中率和数据变化频率调整 Redis TTL。
- 根据线上并发读写情况调整延迟双删间隔，默认 100~500ms。
- 如果多节点缓存失效需要强投递保证，引入 MQ/outbox，不把 Redis Stream 当强一致来源。
