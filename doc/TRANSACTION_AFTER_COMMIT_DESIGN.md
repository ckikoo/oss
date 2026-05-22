# Transaction After-Commit 设计方案

## 1. 背景

当前事务抽象只有 `RunInTx`：

```go
type ITxManager interface {
	RunInTx(ctx context.Context, fn func(context.Context, Tx) error) error
}
```

repo 的缓存失效通常在 DB 写成功后立即执行，例如 bucket/object/cors/event/accesskey/user repo 中的 `invalidate*Cache`。当这些 repo 通过 `WithTx(tx)` 在事务内被调用时，缓存可能在事务提交前被删除。

这会产生经典 cache-aside 竞态：

```text
T1: 开事务，更新 DB，未提交
T1: 删除缓存
T2: 读缓存 miss
T2: 查 DB，读到旧值
T2: 把旧值写回缓存
T1: commit
```

结果是旧值可能在 Redis TTL 内持续存在。视频缓存方案已经确认采用：

```text
同步 after-commit 删除 + 短本地 TTL + 必要位置延迟双删
```

这个能力不应该只服务视频缓存。对象、bucket、用户统计、CORS、事件规则、access key、异步任务入队、视频派生资产清理等都需要统一的事务后副作用机制。

---

## 2. 目标

- DB commit 成功后才执行缓存失效、消息发布、Redis 入队、派生资产清理等副作用。
- DB rollback 时不执行 after-commit hook。
- after-commit hook 同步执行，`RunInTx` 在 hook 执行完成后才返回。
- hook 失败不改变已提交事务结果，只记录日志或由 hook 内部处理。
- 事务内 repo 读操作不读缓存、不写缓存。
- 事务内 repo 写操作不立即删缓存，只登记 after-commit hook。
- 高风险缓存支持延迟双删，兜住并发旧读回填。

非目标：

- 不把核心业务正确性放进 after-commit hook。
- 不让 hook 错误回滚已经提交的 DB 事务。
- 第一阶段不支持复杂嵌套事务 hook 隔离。

---

## 3. API 设计

### 3.1 tx 包新增函数

建议不改 `ITxManager` 接口签名，避免所有调用点连锁修改。新增基于 `context.Context` 的 hook 注册函数：

```go
package tx

import "context"

type AfterCommitFunc func(context.Context)

func AfterCommit(ctx context.Context, fn AfterCommitFunc) bool

func AfterCommitOrNow(ctx context.Context, fn AfterCommitFunc)
```

语义：

- `AfterCommit(ctx, fn)`：如果当前 ctx 在 `RunInTx` 管理的事务中，登记 hook 并返回 `true`；否则返回 `false`。
- `AfterCommitOrNow(ctx, fn)`：如果在事务中则登记 hook，否则立即同步执行。
- `fn` 不返回 error，避免调用方把“事务已提交但 hook 失败”误判为事务失败。
- hook 内部负责记录 Redis/cache/storage 等副作用失败日志。

### 3.2 RunInTx 行为

`RunInTx` 在进入 GORM transaction 前创建 hook 队列，并把队列放入 `txCtx`：

```go
func (t *GormTxManager) RunInTx(ctx context.Context, fn func(context.Context, Tx) error) error {
	state := newAfterCommitState()
	txCtx := context.WithValue(ctx, afterCommitKey{}, state)

	err := t.db.WithContext(txCtx).Transaction(func(gormTx *gorm.DB) error {
		return fn(txCtx, gormTx)
	})
	if err != nil {
		return err
	}

	state.run(ctx) // 同步执行，不起 goroutine
	return nil
}
```

hook 执行顺序按注册顺序 FIFO。这样可以让 repo 层先登记缓存失效，service 层再登记 Redis 入队或资产清理。

### 3.3 afterCommitState

实现建议：

```go
type afterCommitKey struct{}

type afterCommitState struct {
	mu      sync.Mutex
	closed  bool
	hooks   []AfterCommitFunc
}

func (s *afterCommitState) add(fn AfterCommitFunc) bool {
	if fn == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.hooks = append(s.hooks, fn)
	return true
}

func (s *afterCommitState) run(ctx context.Context) {
	s.mu.Lock()
	hooks := append([]AfterCommitFunc(nil), s.hooks...)
	s.closed = true
	s.hooks = nil
	s.mu.Unlock()

	for _, hook := range hooks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// 记录日志，不能 panic 影响已提交事务
				}
			}()
			hook(ctx)
		}()
	}
}
```

hook panic 必须被 recover 并记录日志。DB 已经提交，不能因为 hook panic 让业务进程崩溃。

### 3.4 嵌套事务约束

第一阶段明确不支持复杂嵌套事务隔离。约定：

- 业务代码不要在 `RunInTx` 内再次调用 `RunInTx`。
- `WithTx(tx)` 只传递同一个 GORM tx，不启动新事务。
- 如未来需要嵌套事务/savepoint，after-commit state 需要支持子事务 rollback 时丢弃子作用域 hook。

当前仓库的事务入口主要在 service/timer 层，没有明显需要嵌套 `RunInTx` 的业务模式，因此第一阶段可以先保持简单。

---

## 4. 缓存失效策略

### 4.1 基础规则

所有缓存失效 helper 改成：

```text
如果在事务内：登记 after-commit hook
如果不在事务内：立即同步失效
```

失效动作：

```text
Redis Del -> 本地 Remove -> Redis Stream Publish
```

不要在事务提交前删缓存。

### 4.2 延迟双删

高风险缓存失效 hook 中执行：

```text
commit -> 立即删除一次 -> 100~500ms 后再删除一次
```

示例：

```go
func (r *Repo) invalidateKeysAfterCommit(ctx context.Context, keys ...string) {
	keys = compactKeys(keys)
	if len(keys) == 0 {
		return
	}

	tx.AfterCommitOrNow(ctx, func(runCtx context.Context) {
		r.invalidateKeys(runCtx, keys...)

		time.AfterFunc(200*time.Millisecond, func() {
			timeoutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			r.invalidateKeys(timeoutCtx, keys...)
		})
	})
}
```

注意：

- 第一次删除必须同步执行。
- 第二次删除可以异步延迟执行。
- 延迟删除不要直接使用已经返回的请求 ctx，建议使用带超时的 `context.Background()`。
- 延迟删除失败只记录 warn。
- 延迟间隔必须配置化，不能散落硬编码。

建议配置：

```go
type CacheInvalidationConfig struct {
	DelayedDoubleDeleteEnabled bool
	DelayedDoubleDeleteDelay   time.Duration // 默认 200ms，建议范围 100~500ms
	InvalidateBatchSize        int           // 默认 500
}
```

默认值先取 `200ms`。实际值应基于高并发压测、Redis RTT、DB commit 耗时和 cache miss 回填耗时调整。对低风险缓存可以关闭延迟双删。

### 4.3 哪些缓存需要延迟双删

优先启用：

- object latest/version metadata
- bucket metadata/stat
- user storage stat
- video transcode/profile/encrypt key
- access key 权限相关缓存

可选启用：

- CORS rule
- event rule
- policy rule

不需要：

- 仅本地临时缓存且 TTL 极短的数据
- 无跨请求可见性的计算结果

### 4.4 跨实例失效可靠性

当前缓存管理器使用 Redis Stream 做失效广播，不是 Redis Pub/Sub。它比 Pub/Sub 更适合保留短期消息，但仍然不能作为强一致来源：

- Stream 有 `MaxLen`，旧消息可能被裁剪。
- 实例重启或长时间离线后可能错过历史失效。
- 当前每个实例一个 consumer group，适合“每个实例都消费一份失效消息”，但需要依赖 TTL 兜底。

因此跨实例缓存一致性策略是：

```text
after-commit 同步删本实例 + Redis key
Redis Stream 通知其他实例删除本地 LRU
Redis TTL / 本地 TTL / 延迟双删兜底漏消息和旧读回填
```

如果未来需要更强投递保证，应引入 MQ 或 DB outbox。Redis Keyspace Notification 可作为观测或辅助信号，不建议作为唯一失效机制。

### 4.5 批量失效

批量失效要减少 Redis RTT：

- `DEL` 使用多 key 一次删除。
- key 数量超过 `InvalidateBatchSize` 时分片。
- Redis Stream Publish 一次携带一批 keys，避免逐 key 发布。
- 删除 profile/encrypt key 等场景要先批量查询 key refs，再统一删除。

第一阶段不要求 Lua。只有当失效逻辑需要同时维护 Redis 索引 key、状态 key 或复杂原子条件时，再评估 Lua 脚本。

### 4.6 序列化接口

缓存序列化不要散落在各 repo 里。建议预留统一 codec：

```go
type CacheCodec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
}
```

第一阶段实现用 `encoding/json`，后续基于基准测试替换为 Sonic 或其他实现。

### 4.7 Singleflight 与事务

`singleflight` 只能合并同进程同 key 的缓存 miss，不能解决事务提交前旧读回填问题。规范：

- singleflight key 必须等于 cache key。
- 事务内 repo 读操作禁用缓存和 singleflight，直接查 tx DB。
- 事务外 cache miss 回源 DB 后，写缓存前应检查请求 ctx 是否已取消。
- 高风险缓存仍必须依赖 after-commit 删除和延迟双删。

---

## 5. Repo 改造规范

### 5.1 WithTx 行为

所有带缓存 repo 的 `WithTx` 应遵守：

- 保留 Redis/cache manager/singleflight 字段，便于登记 after-commit。
- 增加 `cacheEnabled bool` 或等价字段。
- tx repo 的读方法绕过缓存，直接查 DB。
- tx repo 的写方法不立即失效，只登记 after-commit。

示例：

```go
func (r *BucketRepo) WithTx(tx tx.Tx) bucket.IBucketRepo {
	db := tx.(*gorm.DB)
	return &BucketRepo{
		db:           db,
		q:            query.Use(db),
		rds:          r.rds,
		cacheManager: r.cacheManager,
		g:            r.g,
		cacheEnabled: false,
	}
}
```

读方法：

```go
func (r *BucketRepo) GetByID(ctx context.Context, id int64) (*do.BucketDo, error) {
	if !r.cacheEnabled {
		return r.getByIDDB(ctx, id)
	}
	return r.getByKey(ctx, consts.BucketCacheKeyByID(id), func() (*do.BucketDo, error) {
		return r.getByIDDB(ctx, id)
	})
}
```

写方法：

```go
func (r *BucketRepo) UpdateBucket(...) (...) {
	// DB update...
	r.invalidateBucketCacheAfterCommit(ctx, userID, id, name)
}
```

---

## 6. Service/Timer 改造范围

### 6.1 事务入口

当前事务入口包括：

- `service/bucket/service.go`
- `service/object/service.go`
- `service/multipart/mutipart.go`
- `service/video/processor.go`
- `service/video/scheduler.go`
- `timer/lifecycle.go`
- `timer/task.go`
- `timer/upload_timeout.go`

这些入口里出现的提交后副作用，都应迁到 after-commit hook。

### 6.2 提交后副作用分类

| 副作用 | 示例 | 处理方式 |
|---|---|---|
| 缓存失效 | bucket/object/user/video cache | repo 内登记 after-commit |
| Redis 入队 | async task enqueue | service 注册 after-commit |
| Redis token 删除 | video play token 删除 | service 注册 after-commit |
| 物理文件/资产清理 | multipart parts、video HLS assets | service 注册 after-commit |
| 事件投递 | object event delivery | 创建 DB outbox 或 after-commit 入队 |
| 日志/指标 | 非关键指标 | 可直接执行，但不要影响事务结果 |

### 6.3 典型迁移

#### Object Put/Delete

对象写入事务内会修改 object、bucket stats、user stats、metering，并可能产生旧对象/旧 multipart/video 派生资产清理。

改造后：

```go
err := txManager.RunInTx(ctx, func(ctx context.Context, tx tx.Tx) error {
	// DB writes through WithTx...

	tx.AfterCommit(ctx, func(runCtx context.Context) {
		// cleanup old physical object / multipart parts / video derived assets
	})
	return nil
})
```

bucket/object/user repo 自己登记缓存失效，service 只登记跨系统副作用。

#### Multipart Complete

事务内创建对象、更新 upload 状态、创建 async task。事务提交后：

- 入队 async task 到 Redis。
- 清理旧对象或旧分片。
- 失效 object/bucket/user cache。

#### Video Processor

事务内更新 profile/transcode、统计和计费。事务提交后：

- 失效 video profile/transcode cache。
- 高风险 video cache 做延迟双删。

#### Lifecycle Timer

生命周期任务通常在 timer 中批量修改对象和统计。事务提交后：

- 失效 object/bucket/user cache。
- 执行视频清理和 play token 删除。
- 清理物理对象或分片。

---

## 7. 实施路线

| 阶段 | 内容 | 优先级 |
|---|---|---|
| Phase 1 | 在 `adaptor/tx` 增加 `AfterCommit` / `AfterCommitOrNow` 和同步执行逻辑 | P0 |
| Phase 2 | 为 after-commit 增加单元测试：commit 执行、rollback 不执行、FIFO、panic recover | P0 |
| Phase 3 | 抽出通用 cache invalidation helper：after-commit + 延迟双删 + 批量分片 | P0 |
| Phase 4 | 改造 object/bucket/user repo 缓存失效 | P0 |
| Phase 5 | 视频缓存按 after-commit 模式实现 | P0 |
| Phase 6 | 改造 accesskey/cors/event repo 缓存失效 | P1 |
| Phase 7 | 迁移 service/timer 中的 Redis 入队、token 删除、物理清理等提交后副作用 | P1 |
| Phase 8 | 增加统一 cache codec 接口，默认实现 `encoding/json` | P1 |
| Phase 9 | 补充并发旧读回填测试和集成测试 | P2 |

---

## 8. 测试清单

### tx 包

- commit 成功后 hook 被同步执行。
- 事务返回 error 时 hook 不执行。
- 事务 panic 被 GORM 回滚时 hook 不执行。
- 多个 hook 按注册顺序执行。
- hook panic 被 recover，不导致进程崩溃。
- `AfterCommitOrNow` 在事务外立即执行。
- `AfterCommit` 在事务外返回 false。

### repo 缓存

- tx repo 读方法不命中 Redis/本地缓存。
- tx repo 写方法在 commit 前不删缓存。
- commit 后缓存被删除。
- rollback 后缓存不被删除。
- 延迟双删能清理第一次删除后被旧读回填的缓存。
- 批量失效按配置分片，Redis `DEL` 和 Stream Publish 不逐 key 往返。
- Redis Stream 失效消息丢失或实例离线时，TTL 和延迟双删能兜底最终收敛。
- singleflight 并发 miss 只产生一次 DB 回源，且事务内读不进入 singleflight。

### service 副作用

- async task DB 创建成功但事务 rollback 时，不入 Redis 队列。
- object/multipart 事务 rollback 时，不清理物理文件。
- video cleanup 事务 rollback 时，不删除 HLS assets 和 play tokens。
- lifecycle 批处理单项失败回滚时，不执行对应 after-commit 副作用。

---

## 9. 风险与约束

- after-commit hook 执行失败时 DB 已提交，只能记录日志和依赖补偿任务。
- 同步 hook 会增加 `RunInTx` 返回延迟，因此 hook 里只能做短操作；长耗时任务应登记异步任务或发送队列。
- 延迟双删会额外增加 Redis 删除和 Stream 消息，需控制 key 数量并做批量删除。
- Redis Stream 失效广播不是强一致机制，不能替代 TTL、延迟双删或补偿任务。
- 批量失效 key 数量过大时必须分片，避免单个 Redis 命令和 Stream 消息过大。
- 请求 ctx 可能在响应后取消，延迟删除必须使用独立的短超时 context。
- 第一阶段不支持嵌套事务 hook 隔离，业务代码应避免嵌套 `RunInTx`。

---

## 10. 设计结论

事务提交前删缓存风险更高，可能把旧数据重新写回 Redis 并持续到 TTL 过期。统一的 after-commit hook 能把缓存失效、Redis 入队、资产清理等副作用绑定到 DB commit 成功之后。

最终策略：

```text
事务内只写 DB 和登记 hook
commit 成功后同步执行 after-commit
高风险缓存立即删 + 延迟双删
rollback 不执行任何提交后副作用
```
