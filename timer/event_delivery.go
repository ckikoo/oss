package timer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"oss/adaptor"
	"oss/adaptor/redis"
	gormEvent "oss/adaptor/repo/event/gorm"
	"oss/consts"
	"oss/service/do"
	"oss/utils/tools"

	"go.uber.org/zap"
)

func handlerEventDeliveries(ctx context.Context, adaptor adaptor.IAdaptor) {
	eventDeliveryRepo := gormEvent.NewEventDeliveryRepo(adaptor.GetGORM())
	eventRuleRepo := gormEvent.NewEventRuleRepo(adaptor)
	eventQueue := redis.NewEventQueue(adaptor)

	deliveryIDs, err := eventQueue.DequeueDeliveryIDs(ctx, 50, time.Second*5)
	if err != nil {
		log.Error("failed to dequeue event delivery IDs", zap.Error(err))
		return
	}

	if len(deliveryIDs) == 0 {
		deliveries, err := eventDeliveryRepo.GetPendingDeliveries(ctx, 50)
		if err != nil {
			log.Error("failed to scan pending event deliveries", zap.Error(err))
			return
		}

		for _, delivery := range deliveries {
			if err := eventQueue.EnqueueDeliveryID(ctx, delivery.ID); err != nil {
				log.Error("failed to enqueue pending delivery ID", zap.Int64("deliveryID", delivery.ID), zap.Error(err))
			}
		}
		return
	}

	for _, deliveryID := range deliveryIDs {
		claimed, delivery, err := eventDeliveryRepo.ClaimEventDelivery(ctx, deliveryID)
		if err != nil {
			log.Error("failed to claim event delivery", zap.Int64("deliveryID", deliveryID), zap.Error(err))
			continue
		}
		if !claimed || delivery == nil {
			log.Warn("skipping unclaimable event delivery", zap.Int64("deliveryID", deliveryID))
			continue
		}

		var rule *do.EventRuleDo
		if delivery.RuleID != 0 {
			rule, err := eventRuleRepo.GetByID(ctx, delivery.RuleID)
			if err != nil || rule == nil {
				log.Error("failed to get event rule", zap.Int64("ruleID", delivery.RuleID), zap.Error(err))
				continue
			}
		}

		err = deliverEvent(ctx, adaptor, rule, delivery)
		update := &do.UpdateEventDelivery{
			Status: &[]int32{consts.EventDeliveryStatusSuccess}[0],
		}

		if err != nil {
			log.Error("failed to deliver event",
				zap.Int64("deliveryID", delivery.ID),
				zap.String("eventType", delivery.EventType),
				zap.Error(err))

			retryCount := delivery.RetryCount + 1
			update.Status = &[]int32{consts.EventDeliveryStatusFailed}[0]
			update.RetryCount = &retryCount
			if retryCount < 3 {
				nextRetry := time.Now().Add(time.Duration(retryCount) * time.Minute)
				update.NextRetryAt = &nextRetry
			}
		}

		if updateErr := eventDeliveryRepo.UpdateEventDelivery(ctx, delivery.ID, update); updateErr != nil {
			log.Error("failed to update event delivery status",
				zap.Int64("deliveryID", delivery.ID), zap.Error(updateErr))
		}
	}
}
func deliverWebhookDirect(ctx context.Context, delivery *do.EventDeliveryDo) error {
	if delivery.ObjectKey == nil {
		return fmt.Errorf("no target url")
	}

	// 复用已有的 webhook 逻辑，构造一个临时 rule
	tempRule := &do.EventRuleDo{
		TargetType: consts.EventTargetTypeWebhook,
		TargetURL:  delivery.ObjectKey,
	}
	return deliverWebhook(ctx, tempRule, delivery)
}
func deliverEvent(ctx context.Context, adaptor adaptor.IAdaptor, rule *do.EventRuleDo, delivery *do.EventDeliveryDo) error {
	if rule == nil {
		return deliverWebhookDirect(ctx, delivery)
	}

	switch rule.TargetType {
	case consts.EventTargetTypeWebhook:
		return deliverWebhook(ctx, rule, delivery)
	case consts.EventTargetTypeMQ, consts.EventTargetTypeRedis:
		return deliverRedis(ctx, adaptor, delivery)
	default:
		return fmt.Errorf("unsupported target type: %s", rule.TargetType)
	}
}

func deliverRedis(ctx context.Context, adaptor adaptor.IAdaptor, delivery *do.EventDeliveryDo) error {
	payload := map[string]interface{}{
		"event_type": delivery.EventType,
		"object_key": delivery.ObjectKey,
		"payload":    delivery.Payload,
		"timestamp":  time.Now().Unix(),
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("oss:event:%d:deliveries", delivery.RuleID)
	if err := adaptor.GetRedis().RPush(ctx, key, string(bodyBytes)).Err(); err != nil {
		return err
	}

	log.Info("Redis MQ delivery queued",
		zap.String("queueKey", key),
		zap.Int64("deliveryID", delivery.ID),
		zap.String("eventType", delivery.EventType))

	return nil
}

func deliverWebhook(ctx context.Context, rule *do.EventRuleDo, delivery *do.EventDeliveryDo) error {
	if rule.TargetURL == nil || *rule.TargetURL == "" {
		return fmt.Errorf("webhook URL not configured")
	}

	payload := map[string]interface{}{
		"event_type": delivery.EventType,
		"object_key": delivery.ObjectKey,
		"payload":    delivery.Payload,
		"timestamp":  time.Now().Unix(),
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, *rule.TargetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	if rule.Secret != nil && *rule.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		stringToSign := strings.Join([]string{timestamp, string(bodyBytes)}, "\n")
		signature := tools.HmacSHA256(stringToSign, *rule.Secret)
		req.Header.Set("X-Event-Timestamp", timestamp)
		req.Header.Set("X-Event-Signature", signature)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook request failed status=%d response=%s", resp.StatusCode, string(respBody))
	}

	truncatedResponse := string(respBody)
	if len(truncatedResponse) > 200 {
		truncatedResponse = truncatedResponse[:200] + "...(truncated)"
	}

	log.Info("Webhook delivery success",
		zap.String("url", *rule.TargetURL),
		zap.String("eventType", delivery.EventType),
		zap.String("status", resp.Status),
		zap.String("responseBody", truncatedResponse),
		zap.Int("responseBodyLength", len(respBody)))

	return nil
}
