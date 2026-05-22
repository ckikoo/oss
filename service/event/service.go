package event

import (
	"context"
	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/bucket"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	"oss/adaptor/repo/event"
	"oss/adaptor/repo/event/gorm"
	"oss/common"
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"
	"oss/utils/logger"
	"strings"

	"github.com/gogf/gf/util/gconv"
	"go.uber.org/zap"
)

type Service struct {
	eventRuleRepo     event.IEventRuleRepo
	eventDeliveryRepo event.IEventDeliveryRepo
	eventQueue        redis.IEventQueue
	bucketRepo        bucket.IBucketRepo
	logger            *zap.Logger
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		eventRuleRepo:     gorm.NewEventRuleRepo(adaptor),
		eventDeliveryRepo: gorm.NewEventDeliveryRepo(adaptor.GetGORM()),
		eventQueue:        redis.NewEventQueue(adaptor),
		bucketRepo:        gormBucket.NewBucketRepo(adaptor),
		logger:            logger.GetLogger().With(zap.String("module", "event")),
	}
}

// CreateEventRule 创建事件规则
func (srv *Service) CreateEventRule(ctx *common.UserInfoCtx, req *dto.CreateEventRuleReq) (*dto.CreateEventRuleResp, common.Errno) {
	// 校验事件类型
	if !srv.validateEventTypes(req.Events) {
		return nil, common.ErrInvalidParams
	}

	// 校验目标类型
	if !srv.validateTargetType(req.TargetType) {
		return nil, common.ErrInvalidParams
	}

	info, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, req.BucketName)
	if err != nil {
		srv.logger.Error("failed to get bucket", zap.Error(err))
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if info == nil {
		return nil, common.BucketNotFoundErr
	}

	// 检查规则名是否已存在
	existing, err := srv.eventRuleRepo.GetByBucketIDAndRuleName(ctx, info.ID, req.RuleName)
	if err != nil {
		srv.logger.Error("failed to check existing rule", zap.Error(err))
		return nil, common.ErrInternalServer
	}
	if existing != nil {
		return nil, common.EventRuleAlreadyExists
	}

	rule := &do.EventRuleDo{
		BucketID:   info.ID,
		RuleName:   req.RuleName,
		Events:     strings.Join(req.Events, ","),
		Prefix:     req.Prefix,
		Suffix:     req.Suffix,
		TargetType: req.TargetType,
		TargetURL:  req.TargetURL,
		Secret:     req.Secret,
		Status:     consts.EventRuleStatusEnabled,
	}

	ruleID, err := srv.eventRuleRepo.CreateEventRule(ctx, rule)
	if err != nil {
		srv.logger.Error("failed to get event rule", zap.Error(err))
		return nil, common.ErrInternalServer
	}

	return &dto.CreateEventRuleResp{
		RuleID: ruleID,
	}, common.OK
}

// ListEventRules 列出事件规则
func (srv *Service) ListEventRules(ctx *common.UserInfoCtx, bucketName string) (*dto.ListEventRulesResp, common.Errno) {
	info, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		srv.logger.Error("failed to get bucket", zap.Error(err))
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if info == nil {
		return nil, common.BucketNotFoundErr
	}

	rules, err := srv.eventRuleRepo.ListByBucketID(ctx, info.ID)
	if err != nil {
		srv.logger.Error("failed to list event rules", zap.Error(err))
		return nil, common.ErrInternalServer
	}

	resp := &dto.ListEventRulesResp{
		Rules: make([]*dto.EventRuleInfo, 0, len(rules)),
	}

	for _, rule := range rules {
		resp.Rules = append(resp.Rules, &dto.EventRuleInfo{
			RuleID:     rule.ID,
			RuleName:   rule.RuleName,
			Events:     strings.Split(rule.Events, ","),
			Prefix:     rule.Prefix,
			Suffix:     rule.Suffix,
			TargetType: rule.TargetType,
			TargetURL:  rule.TargetURL,
			Status:     rule.Status,
			CreatedAt:  rule.CreatedAt,
			UpdatedAt:  rule.UpdatedAt,
		})
	}

	return resp, common.OK
}

// UpdateEventRule 更新事件规则
func (srv *Service) UpdateEventRule(ctx *common.UserInfoCtx, bucketName string, ruleID int64, req *dto.UpdateEventRuleReq) common.Errno {
	// 校验事件类型
	if req.Events != nil && !srv.validateEventTypes(*req.Events) {
		return common.ErrInvalidParams
	}

	// 校验目标类型
	if req.TargetType != nil && !srv.validateTargetType(*req.TargetType) {
		return common.ErrInvalidParams
	}

	info, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		srv.logger.Error("failed to get bucket", zap.Error(err))
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if info == nil {
		return common.BucketNotFoundErr
	}

	rule, err := srv.eventRuleRepo.GetByID(ctx, ruleID)
	if err != nil {
		srv.logger.Error("failed to get event rule", zap.Error(err))
		return common.ErrnoFromRepoErrorWithNotFound(err, common.ErrInternalServer, common.EventRuleNotFound)
	}
	if rule == nil || rule.BucketID != info.ID {
		return common.EventRuleNotFound
	}

	update := &do.UpdateEventRule{}
	if req.RuleName != nil {
		update.RuleName = req.RuleName
	}
	if req.Events != nil {
		events := strings.Join(*req.Events, ",")
		update.Events = &events
	}
	if req.Prefix != nil {
		update.Prefix = req.Prefix
	}
	if req.Suffix != nil {
		update.Suffix = req.Suffix
	}
	if req.TargetType != nil {
		update.TargetType = req.TargetType
	}
	if req.TargetURL != nil {
		update.TargetURL = req.TargetURL
	}
	if req.Secret != nil {
		update.Secret = req.Secret
	}
	if req.Status != nil {
		update.Status = req.Status
	}

	err = srv.eventRuleRepo.UpdateEventRule(ctx, ruleID, update)
	if err != nil {
		srv.logger.Error("failed to update event rule", zap.Error(err))
		return common.ErrInternalServer
	}

	return common.OK
}

// DeleteEventRule 删除事件规则
func (srv *Service) DeleteEventRule(ctx *common.UserInfoCtx, bucketName string, ruleID int64) common.Errno {
	info, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		srv.logger.Error("failed to get bucket", zap.Error(err))
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if info == nil {
		return common.BucketNotFoundErr
	}

	rule, err := srv.eventRuleRepo.GetByID(ctx, ruleID)
	if err != nil {
		srv.logger.Error("failed to get event rule", zap.Error(err))
		return common.ErrnoFromRepoErrorWithNotFound(err, common.ErrInternalServer, common.EventRuleNotFound)
	}
	if rule == nil || rule.BucketID != info.ID {
		return common.EventRuleNotFound
	}

	err = srv.eventRuleRepo.DeleteEventRule(ctx, ruleID)
	if err != nil {
		srv.logger.Error("failed to delete event rule", zap.Error(err))
		return common.ErrInternalServer
	}

	return common.OK
}

// TriggerEvent 触发事件
func (srv *Service) TriggerEvent(ctx context.Context, bucketID int64, eventType string, objectKey string, payload map[string]interface{}) {
	// 获取匹配的规则
	rules, err := srv.eventRuleRepo.ListActiveRulesByBucketID(ctx, bucketID)
	if err != nil {
		srv.logger.Error("failed to list active event rules", zap.Error(err))
		return
	}

	for _, rule := range rules {
		// 检查事件类型是否匹配
		if !srv.matchEventType(rule.Events, eventType) {
			continue
		}

		// 检查对象键是否匹配前缀/后缀
		if !srv.matchObjectKey(rule.Prefix, rule.Suffix, objectKey) {
			continue
		}

		// 创建投递记录
		delivery := &do.EventDeliveryDo{
			RuleID:    rule.ID,
			EventType: eventType,
			ObjectKey: &objectKey,
			Payload:   gconv.String(payload),
			Status:    consts.EventDeliveryStatusPending,
		}

		deliveryID, err := srv.eventDeliveryRepo.CreateEventDelivery(ctx, delivery)
		if err != nil {
			srv.logger.Error("failed to create event delivery", zap.Error(err))
			continue
		}

		if err := srv.eventQueue.EnqueueDeliveryID(ctx, deliveryID); err != nil {
			srv.logger.Error("failed to enqueue event delivery trigger", zap.Int64("delivery_id", deliveryID), zap.Error(err))
		}
	}
}

// 辅助方法

func (srv *Service) validateEventTypes(events []string) bool {
	validTypes := map[string]bool{
		consts.EventTypePutObject:           true,
		consts.EventTypeGetObject:           true,
		consts.EventTypeDeleteObject:        true,
		consts.EventTypeMultipartComplete:   true,
		consts.EventTypeLifecycleTransition: true,
		consts.EventTypeLifecycleExpiration: true,
	}

	for _, event := range events {
		if !validTypes[event] {
			return false
		}
	}
	return true
}

func (srv *Service) validateTargetType(targetType string) bool {
	validTypes := map[string]bool{
		consts.EventTargetTypeWebhook: true,
		consts.EventTargetTypeMQ:      true,
		consts.EventTargetTypeRedis:   true,
		consts.EventTargetTypeFunc:    true,
	}
	return validTypes[targetType]
}

func (srv *Service) matchEventType(ruleEvents string, eventType string) bool {
	events := strings.Split(ruleEvents, ",")
	for _, e := range events {
		if strings.TrimSpace(e) == eventType {
			return true
		}
	}
	return false
}

func (srv *Service) matchObjectKey(prefix, suffix *string, objectKey string) bool {
	if prefix != nil && !strings.HasPrefix(objectKey, *prefix) {
		return false
	}
	if suffix != nil && !strings.HasSuffix(objectKey, *suffix) {
		return false
	}
	return true
}
