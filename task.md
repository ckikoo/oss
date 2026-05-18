# Async Task 完整数据流

## 触发层

业务操作完成后，同步调用 `Enqueue()`，不再使用 `go func()`。

```
DeleteObject / CompleteMultipart / PutObject
          ↓
asyncTaskService.Enqueue()
  ├─ INSERT async_tasks  status=PENDING
  ├─ uk_biz(task_type, biz_id) 唯一索引天然幂等，重复调用直接忽略
  ├─ UPDATE async_tasks status=QUEUED
  └─ Redis LIST RPUSH taskID（失败不影响主流程，recoverStuckQueued 兜底）
```

---

## 状态定义

| 值 | 状态 | 含义 |
|---|---|---|
| 0 | PENDING | 已写 DB，等 timer 扫描入队 |
| 1 | QUEUED | 已入 Redis LIST，等 worker 消费 |
| 2 | RUNNING | worker 取走，执行中（Redis task lock 存在） |
| 3 | COMPLETED | 执行成功 |
| 4 | FAILED | 重试耗尽，彻底失败 |

---

## 存储层

```
MySQL async_tasks          Redis LIST
─────────────────          ──────────
source of truth            加速通道
status 状态流转            RPUSH taskID
retry / updated_at         BLPOP 阻塞消费
兜底扫描                   丢了不影响正确性
```

---

## Timer 层（三个定时任务）

### 1. scanPending · 每 5s

```
SELECT * FROM async_tasks
WHERE status=PENDING
ORDER BY id
LIMIT 50
FOR UPDATE SKIP LOCKED

for each task:
  UPDATE status=QUEUED
  Redis RPUSH taskID        ← 失败没关系，recoverStuckQueued 会兜底
```

### 2. recoverStuckQueued · 每 1min

```
SELECT * FROM async_tasks
WHERE status=QUEUED
AND updated_at < NOW() - 2min   ← QUEUED 太久说明 Redis 已丢
ORDER BY updated_at, id
LIMIT 50
FOR UPDATE SKIP LOCKED

for each task:
  UPDATE status=PENDING          ← 重置，等 scanPending 重新入队
```

### 3. recoverStuckRunning · 每 1min

```
SELECT * FROM async_tasks
WHERE status=RUNNING
ORDER BY id
LIMIT 50
FOR UPDATE SKIP LOCKED

for each task:
  Redis task lock 不存在 → 再次按 id/status FOR UPDATE SKIP LOCKED → UPDATE status=PENDING
```

---

## Worker 消费流程

```
Redis BLPOP taskID（阻塞等待，超时 5s）
  │
  ├─ 无数据 → 退出本轮，等下次
  │
  └─ 取到 taskID
       ↓
     查 DB：status == QUEUED？
       ├─ 否（已被其他 worker 取走）→ 跳过
       └─ 是
            ↓
          Redis SETNX task lock ttl=30s
          UPDATE status=RUNNING
            ↓
          executeTask()
            ├─ TriggerEvent
            ├─ PhysicalMerge
            ├─ AbortMultipart
            └─ ...
            ↓
       ┌────┴────┐
     成功        失败
       ↓           ↓
  COMPLETED    retry_count++
  last_error=err
               │
               ├─ retry_count < max_retry
               │    └─ UPDATE status=PENDING
               │         等 scanPending 重新入队
               │
               └─ retry_count >= max_retry
                    └─ UPDATE status=FAILED
```

---

## 完整状态流转

```
Enqueue()
    ↓
 PENDING  ←─────────────────────────┐
    ↓ scanPending (每 5s)            │
 QUEUED ←── recoverStuckQueued ─────┤ (Redis 丢失兜底)
    ↓ worker 消费                    │
 RUNNING ←── recoverStuckRunning ───┤ (worker 崩溃兜底)
    ↓                                │
 ┌──┴──┐                            │
 ↓     ↓                            │
DONE  FAILED ── retry 未耗尽 ───────┘
       ↓
    FAILED（retry 耗尽，终态）
```

---

## Redis 丢失场景

```
正常路径：
  PENDING → QUEUED(DB+Redis) → RUNNING → DONE

Redis 崩溃后：
  QUEUED(DB only) → 超过 2min → recoverStuckQueued
                                      ↓
                                   PENDING → scanPending 重新入队
```

DB 永远是 source of truth，Redis 只是加速层，丢了自动恢复。

---

## ZSET 去重为何不再需要

旧方案（无 QUEUED 状态）：

```
timer 第 1 轮扫 → task 仍是 PENDING → 重复入队
timer 第 2 轮扫 → task 仍是 PENDING → 再次入队  ← 需要 ZSET 去重
```

新方案：

```
timer 扫描 PENDING → 改为 QUEUED（原子操作）→ 下轮只扫 PENDING
QUEUED 的任务不会被再次扫到 → 天然去重 → 直接用 LIST 即可
```
