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
| `task` | `handlerTask` | 30s |
| `task-recovery` | `handlerTaskRecovery` | 1m |
| `upload-timeout` | `handlerUploadMergeTimeout` | 30s |
| `lifecycle` | `handlerLifecycleEvents` | 1m |
| `event-delivery` | `handlerEventDeliveries` | 10s |
| `scan-lifecycle` | `handlerScanTableLifecycleEvents` | 1m |

传入未知 `mode` 时，`timer.StartTimerMode` 会返回错误并输出当前支持的 mode 列表。

## 异步任务队列语义

`async_tasks` 是任务状态源，保存任务类型、状态、失败原因和恢复依据；Redis 队列只保存 `task_id`，用于唤醒 worker 快速消费。

`handlerTask` 只消费 Redis 队列，不直接扫描数据库。`handlerTaskRecovery` 单独定时扫描 `async_tasks` 中的 pending 任务并批量重新入 Redis，用于补偿 Redis 入队失败、进程重启或队列丢失。

## 入口职责

`cmd/task/main.go` 负责：

1. 注册 `-mode` 参数。
2. 调用 `config.InitConfig()` 解析 `-mode`、`-c`、`-e`。
3. 初始化 MySQL、Redis 和 adaptor。
4. 监听 `SIGINT` / `SIGTERM`，通过 `context` 停止任务循环。
5. 调用 `timer.StartTimerMode` 启动指定任务。

MySQL 和 Redis 初始化逻辑已抽到 `internal/bootstrap`，供 API 入口和任务入口复用。
