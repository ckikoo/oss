package gorm

import (
	"context"
	"oss/adaptor/repo/event"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"time"

	"gorm.io/gorm"
)

type eventDeliveryRepo struct {
	db *gorm.DB
	q  *query.Query
}

var _ event.IEventDeliveryRepo = (*eventDeliveryRepo)(nil)

func NewEventDeliveryRepo(db *gorm.DB) event.IEventDeliveryRepo {
	return &eventDeliveryRepo{db: db, q: query.Use(db)}
}

func (r *eventDeliveryRepo) WithTx(tx tx.Tx) event.IEventDeliveryRepo {
	return &eventDeliveryRepo{db: tx.(*gorm.DB), q: query.Use(tx.(*gorm.DB))}
}
func (r *eventDeliveryRepo) CreateEventDelivery(ctx context.Context, delivery *do.EventDeliveryDo) (int64, error) {
	q := r.q.EventDelivery

	model := &model.EventDelivery{
		RuleID:    delivery.RuleID,
		EventType: delivery.EventType,
		ObjectKey: delivery.ObjectKey,
		Payload:   delivery.Payload,
		Status:    delivery.Status,
	}

	err := q.WithContext(ctx).Create(model)
	if err != nil {
		return 0, repoerr.Wrap(err)
	}

	return model.ID, nil
}

func (r *eventDeliveryRepo) GetPendingDeliveries(ctx context.Context, limit int) ([]*do.EventDeliveryDo, error) {
	var models []model.EventDelivery
	now := time.Now()
	err := r.db.WithContext(ctx).
		Where("status = ? OR (status = ? AND next_retry_at <= ?)", consts.EventDeliveryStatusPending, consts.EventDeliveryStatusFailed, now).
		Order("created_at ASC").
		Limit(limit).
		Find(&models).Error
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	deliveries := make([]*do.EventDeliveryDo, 0, len(models))
	for _, model := range models {
		deliveries = append(deliveries, &do.EventDeliveryDo{
			ID:           model.ID,
			RuleID:       model.RuleID,
			EventType:    model.EventType,
			ObjectKey:    model.ObjectKey,
			Payload:      model.Payload,
			Status:       model.Status,
			RetryCount:   model.RetryCount,
			ResponseCode: model.ResponseCode,
			ResponseBody: model.ResponseBody,
			NextRetryAt:  model.NextRetryAt,
			CreatedAt:    model.CreatedAt,
			UpdatedAt:    model.UpdatedAt,
		})
	}

	return deliveries, nil
}

func (r *eventDeliveryRepo) GetEventDeliveryByID(ctx context.Context, deliveryID int64) (*do.EventDeliveryDo, error) {
	var modelData model.EventDelivery
	err := r.db.WithContext(ctx).First(&modelData, deliveryID).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, repoerr.Wrap(err)
	}

	return &do.EventDeliveryDo{
		ID:           modelData.ID,
		RuleID:       modelData.RuleID,
		EventType:    modelData.EventType,
		ObjectKey:    modelData.ObjectKey,
		Payload:      modelData.Payload,
		Status:       modelData.Status,
		RetryCount:   modelData.RetryCount,
		ResponseCode: modelData.ResponseCode,
		ResponseBody: modelData.ResponseBody,
		NextRetryAt:  modelData.NextRetryAt,
		CreatedAt:    modelData.CreatedAt,
		UpdatedAt:    modelData.UpdatedAt,
	}, nil
}

func (r *eventDeliveryRepo) UpdateEventDelivery(ctx context.Context, deliveryID int64, update *do.UpdateEventDelivery) error {
	q := r.q.EventDelivery
	model := make(map[string]interface{})

	if update.Status != nil {
		model[q.Status.ColumnName().String()] = *update.Status
	}
	if update.RetryCount != nil {
		model[q.RetryCount.ColumnName().String()] = *update.RetryCount
	}
	if update.ResponseCode != nil {
		model[q.ResponseCode.ColumnName().String()] = update.ResponseCode
	}
	if update.ResponseBody != nil {
		model[q.ResponseBody.ColumnName().String()] = update.ResponseBody
	}
	if update.NextRetryAt != nil {
		model[q.NextRetryAt.ColumnName().String()] = update.NextRetryAt
	}

	_, err := q.WithContext(ctx).Where(q.ID.Eq(deliveryID)).Updates(model)
	if err != nil {
		return repoerr.Wrap(err)
	}
	return nil
}

func (r *eventDeliveryRepo) DeleteEventDelivery(ctx context.Context, deliveryID int64) error {
	q := r.q.EventDelivery
	_, err := q.WithContext(ctx).Where(q.ID.Eq(deliveryID)).Delete()
	return repoerr.Wrap(err)
}
