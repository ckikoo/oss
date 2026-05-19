# 任务进程启动说明

后台任务已经从 API 主进程拆出，独立入口为：

```text
cmd/task/main.go
```

API 服务入口 `main.go` 只负责启动 Hertz HTTP 服务，不再启动 `timer.StartTimer`。

## 启动命令

默认启动全部后台任务：

```bash
go run ./cmd/task -mode all
```

指定配置文件：

```bash
go run ./cmd/task -c ./config.yaml -mode all
```

使用 Makefile：

```bash
make run-task
make build-task
```

指定 Makefile 任务模式：

```bash
make run-task TASK_MODE=lifecycle
```

## flag 参数

`cmd/task/main.go` 使用标准库 `flag` 注册任务参数，并复用 `config.InitConfig()` 统一解析参数。

| 参数 | 默认值 | 说明 |
|---|---:|---|
| `-mode` | `all` | 选择启动哪一类后台任务 |
| `-c` | `./config.yaml` | 配置文件路径，由 `config` 包注册 |
| `-e` | `ETCD_ADDR` | Etcd 地址，由 `config` 包注册 |

## mode 与函数映射

入口统一调用：

```go
timer.StartTimerMode(ctx, adaptor, timer.Mode(*mode))
```

| mode | 内部任务函数 | 间隔 |
|---|---|---:|
| `all` | 启动全部任务函数 | 按各任务自身间隔 |
| `task` | `handlerTask` | 5s |
| `task-recovery` | 启动全部 async 维护任务（兼容模式） | 按各任务自身间隔 |
| `task-scan-pending` | `handlerScanPendingAsyncTasks` | 5s |
| `task-recover-queued` | `handlerRecoverStaleQueuedAsyncTasks` | 1m |
| `task-recover-running` | `handlerRecoverStaleRunningAsyncTasks` | 1m |
| `upload-timeout` | `handlerUploadMergeTimeout` | 30s |
| `lifecycle` | `handlerLifecycleEvents` | 1m |
| `event-delivery` | `handlerEventDeliveries` | 10s |
| `scan-lifecycle` | `handlerScanTableLifecycleEvents` | 1m |

传入未知 `mode` 时，`timer.StartTimerMode` 会返回错误并输出当前支持的 mode 列表。

## 异步任务队列语义

`async_tasks` 是任务状态源，保存任务类型、状态、失败原因和恢复依据；Redis LIST 队列只保存 `async_tasks.id`，用于唤醒 worker 快速消费。

Redis 使用 LIST 作为 ready queue：

```text
key: oss:task:ready
item: async_tasks.id
```

任务写入 DB 后先从 `PENDING` 改为 `QUEUED` 再 `RPUSH` 到 Redis。LIST 本身不负责去重，worker 消费时必须通过 DB 的 `QUEUED -> RUNNING` 条件更新抢占任务；重复 LIST item 会因为 DB 状态不匹配被跳过。

任务 ID 使用 `async_tasks.id`，不再额外生成 UUID。业务 ID 不直接作为任务 ID 使用，例如 `upload_id` 只表示分片上传会话，`async_tasks.id` 表示一次异步调度执行。业务幂等通过数据库唯一约束约束业务维度，例如 `task_type + biz_id`。

入队示例：

```text
RPUSH oss-server:task:ready 1001
```

worker 使用 `BLPOP` 阻塞弹出任务 ID，并使用 Redis task lock 维护执行租约。async 维护任务拆成三个独立 timer：`task-scan-pending` 通过 `FOR UPDATE SKIP LOCKED` 扫描 `PENDING` 任务转为 `QUEUED` 并入队；如果 Redis 入队失败，会立即把仍为 `QUEUED` 的任务按条件重置为 `PENDING`；`task-recover-queued` 通过 `FOR UPDATE SKIP LOCKED` 将超过 2 分钟未消费的 `QUEUED` 任务重置为 `PENDING`；`task-recover-running` 扫描 `RUNNING` 后检查 Redis task lock，确认锁不存在时再次按行加 `FOR UPDATE SKIP LOCKED` 并重置为 `PENDING`。

`handlerTask` 只消费 Redis LIST 队列，不直接扫描数据库。拿到任务 ID 后必须先通过 `FOR UPDATE SKIP LOCKED` 抢占 `async_tasks` 行，只有抢占成功的 worker 才执行。三个 async 维护 timer 分别补偿 Redis 入队失败、进程重启、队列丢失或 worker 崩溃；`task-recovery` mode 仅作为兼容入口，一次启动这三个维护 timer。

## 入口职责

`cmd/task/main.go` 负责：

1. 注册 `-mode` 参数。
2. 调用 `config.InitConfig()` 解析 `-mode`、`-c`、`-e`。
3. 初始化 MySQL、Redis 和 adaptor。
4. 监听 `SIGINT` / `SIGTERM`，通过 `context` 停止任务循环。
5. 调用 `timer.StartTimerMode` 启动指定任务。

MySQL 和 Redis 初始化逻辑已抽到 `internal/bootstrap`，供 API 入口和任务入口复用。
