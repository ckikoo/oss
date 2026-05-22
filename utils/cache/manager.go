package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2"
	"go.uber.org/zap"
)

const (
	defaultStreamName    = "cache:invalidation:stream"
	defaultLocalCapacity = 5000
	defaultLocalTTL      = 30 * time.Second
	streamReadTimeout    = 5 * time.Second
	streamReadCount      = 50
)

type ManagerConfig struct {
	StreamName    string
	LocalCapacity int
	LocalTTL      time.Duration
}

func defaultConfig() ManagerConfig {
	return ManagerConfig{
		StreamName:    defaultStreamName,
		LocalCapacity: defaultLocalCapacity,
		LocalTTL:      defaultLocalTTL,
	}
}

type Manager struct {
	cfg        ManagerConfig
	rds        *redis.Client
	workerID   string
	localCache *lru.Cache[string, *Entry]

	stopCh chan struct{}
	once   sync.Once
	logger *zap.Logger
}

var _ IManager = (*Manager)(nil)
var _ ISubscriber = (*Manager)(nil)

func NewManager(rds *redis.Client, logger *zap.Logger, opts ...func(*ManagerConfig)) *Manager {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	cache, _ := lru.New[string, *Entry](cfg.LocalCapacity)

	return &Manager{
		cfg:        cfg,
		rds:        rds,
		workerID:   uuid.New().String(),
		localCache: cache,
		stopCh:     make(chan struct{}),
		logger:     logger,
	}
}

// ---- ILocalCache ----

func (m *Manager) Get(key string) (*Entry, bool) {
	entry, ok := m.localCache.Get(key)
	if !ok || entry.Expired() {
		if ok {
			m.localCache.Remove(key) // 懒删除
		}
		return nil, false
	}
	return entry, true
}

func (m *Manager) Set(key string, value any, ttl time.Duration) {
	if ttl <= 0 {
		ttl = m.cfg.LocalTTL
	}
	m.localCache.Add(key, NewEntry(value, ttl))
}

func (m *Manager) Remove(keys ...string) {
	for _, key := range keys {
		m.localCache.Remove(key)
	}
}

// ---- IPublisher ----

func (m *Manager) Publish(ctx context.Context, keys ...string) error {
	msg := InvalidationMsg{
		Keys:      keys,
		SenderID:  m.workerID,
		PublishAt: time.Now(),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("cache publish marshal: %w", err)
	}

	return m.rds.XAdd(ctx, &redis.XAddArgs{
		Stream: m.cfg.StreamName,
		// 只保留最近 10000 条，防止 stream 无限增长
		MaxLen: 10000,
		Approx: true,
		Values: map[string]interface{}{"data": string(data)},
	}).Err()
}

// ---- ISubscriber ----

func (m *Manager) Start(ctx context.Context) error {
	if err := m.cleanStaleGroups(ctx); err != nil {
		m.logger.Warn("clean stale groups failed", zap.Error(err))
		// 不影响主流程，继续
	}

	err := m.rds.XGroupCreateMkStream(ctx, m.cfg.StreamName, m.workerID, "$").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("cache subscriber create group: %w", err)
	}

	go m.consume(ctx)
	return nil
}

func (m *Manager) Stop() {
	m.once.Do(func() {
		close(m.stopCh)
	})
}

func (m *Manager) consume(ctx context.Context) {
	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		msgs, err := m.rds.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    m.workerID,
			Consumer: "main",
			Streams:  []string{m.cfg.StreamName, ">"},
			Count:    streamReadCount,
			Block:    streamReadTimeout,
		}).Result()

		if err != nil {
			if err == redis.Nil {
				continue // 超时无消息，正常
			}
			// m.logger.Warn("cache subscriber read error", zap.Error(err))
			time.Sleep(time.Second) // 报错退避
			continue
		}

		if len(msgs) == 0 {
			continue
		}

		for _, msg := range msgs[0].Messages {
			m.handle(ctx, msg)
		}
	}
}

func (m *Manager) handle(ctx context.Context, msg redis.XMessage) {
	// 无论如何都 ACK，防止消息堆积
	defer m.rds.XAck(ctx, m.cfg.StreamName, m.workerID, msg.ID)

	raw, ok := msg.Values["data"].(string)
	if !ok {
		return
	}

	var payload InvalidationMsg
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		m.logger.Warn("cache subscriber unmarshal error", zap.Error(err))
		return
	}

	// 自己发的，跳过（写时已主动删）
	if payload.SenderID == m.workerID {
		return
	}

	// 消息超过本地 TTL，本地缓存早已自然过期，跳过
	if time.Since(payload.PublishAt) > m.cfg.LocalTTL {
		m.logger.Debug("skip stale invalidation msg",
			zap.Strings("keys", payload.Keys),
			zap.Duration("age", time.Since(payload.PublishAt)),
		)
		return
	}

	m.Remove(payload.Keys...)
}

func (m *Manager) cleanStaleGroups(ctx context.Context) error {
	// 获取所有 group
	groups, err := m.rds.XInfoGroups(ctx, m.cfg.StreamName).Result()
	if err != nil {
		return err
	}

	for _, group := range groups {
		// 跳过自己
		if group.Name == m.workerID {
			continue
		}

		// 获取该 group 下的消费者
		consumers, err := m.rds.XInfoConsumers(ctx, m.cfg.StreamName, group.Name).Result()
		if err != nil {
			continue
		}

		// 判断是否僵尸：没有消费者 or 所有消费者超过 5 分钟没活跃
		isStale := len(consumers) == 0
		if !isStale {
			allIdle := true
			for _, c := range consumers {
				if time.Duration(c.Idle)*time.Millisecond < 5*time.Minute {
					allIdle = false
					break
				}
			}
			isStale = allIdle
		}

		if isStale {
			m.rds.XGroupDestroy(ctx, m.cfg.StreamName, group.Name)
			m.logger.Info("destroyed stale group", zap.String("group", group.Name))
		}
	}
	return nil
}
