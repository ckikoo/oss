package bucket

import (
	"context"
	"time"

	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/consts"
	"oss/service/do"

	"gorm.io/gorm"
)

type BucketRepo struct {
	db *gorm.DB
}

var _ IBucketRepo = (*BucketRepo)(nil)

func NewBucketRepo(db *gorm.DB) *BucketRepo {
	return &BucketRepo{db: db}
}

func (r *BucketRepo) toBucketDo(modelBucket *model.Bucket) *do.BucketDo {
	return &do.BucketDo{
		ID:           modelBucket.ID,
		UserID:       modelBucket.UserID,
		Name:         modelBucket.Name,
		Region:       modelBucket.Region,
		Acl:          modelBucket.Acl,
		Versioning:   modelBucket.Versioning,
		Status:       modelBucket.Status,
		StorageClass: modelBucket.StorageClass,
		ObjectCount:  modelBucket.ObjectCount,
		StorageSize:  modelBucket.StorageSize,
		CreatedAt:    modelBucket.CreatedAt,
		UpdatedAt:    modelBucket.UpdatedAt,
	}
}

func (r *BucketRepo) CreateBucket(ctx context.Context, bucket *do.CreateBucket) (int64, error) {
	var err error

	ia := consts.StorageClassIA
	archive := consts.StorageClassArchive
	transitionDays30 := int32(30)
	transitionDays90 := int32(90)
	expirationDays180 := int32(180)

	modelBucket := &model.Bucket{
		UserID:       bucket.UserID,
		Name:         bucket.Name,
		Region:       bucket.Region,
		Acl:          bucket.Acl,
		Versioning:   bucket.Versioning,
		Status:       consts.BucketStatusNormal,
		StorageClass: bucket.StorageClass,
		ObjectCount:  0,
		StorageSize:  0,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err = tx.Model(&model.Bucket{}).WithContext(ctx).Create(modelBucket).Error; err != nil {
			return err
		}
		defaultRules := []*do.CreateLifecycleRule{
			{
				BucketID:               modelBucket.ID,
				RuleName:               "Default-IA-Transition",
				Status:                 1,
				Prefix:                 nil,
				TransitionDays:         &transitionDays30,
				TransitionStorageClass: &ia,
				ExpirationDays:         nil,
			},
			{
				BucketID:               modelBucket.ID,
				RuleName:               "Default-Archive-Transition",
				Status:                 1,
				Prefix:                 nil,
				TransitionDays:         &transitionDays90,
				TransitionStorageClass: &archive,
				ExpirationDays:         nil,
			},
			{
				BucketID:               modelBucket.ID,
				RuleName:               "Default-Expiration",
				Status:                 1,
				Prefix:                 nil,
				TransitionDays:         nil,
				TransitionStorageClass: nil,
				ExpirationDays:         &expirationDays180,
			},
		}

		return tx.Model(&model.LifecycleRule{}).CreateInBatches(defaultRules, 3).Error
	})
	if err != nil {
		return 0, err
	}

	return modelBucket.ID, nil
}

func (r *BucketRepo) GetByName(ctx context.Context, userID int64, name string) (*do.BucketDo, error) {
	return r.GetByUserAndName(ctx, userID, name)
}

func (r *BucketRepo) GetByUserAndName(ctx context.Context, userID int64, name string) (*do.BucketDo, error) {
	q := query.Use(r.db)
	modelBucket, err := q.Bucket.WithContext(ctx).Where(q.Bucket.UserID.Eq(userID), q.Bucket.Name.Eq(name)).First()
	if err != nil {
		return nil, err
	}
	return r.toBucketDo(modelBucket), nil
}

func (r *BucketRepo) GetByID(ctx context.Context, id int64) (*do.BucketDo, error) {
	q := query.Use(r.db)
	modelBucket, err := q.Bucket.WithContext(ctx).Where(q.Bucket.ID.Eq(id)).First()
	if err != nil {
		return nil, err
	}
	return r.toBucketDo(modelBucket), nil
}

func (r *BucketRepo) ListByFilter(ctx context.Context, userID int64, status int32) ([]*do.BucketDo, error) {
	q := query.Use(r.db)
	qs := q.Bucket.WithContext(ctx).Where(q.Bucket.UserID.Eq(userID))
	if status > 0 {
		qs = qs.Where(q.Bucket.Status.Eq(status))
	} else {
		qs = qs.Where(q.Bucket.Status.Neq(consts.BucketStatusDeleted))
	}

	modelBuckets, err := qs.Order(q.Bucket.ID.Desc()).Find()
	if err != nil {
		return nil, err
	}

	buckets := make([]*do.BucketDo, len(modelBuckets))
	for i, modelBucket := range modelBuckets {
		buckets[i] = r.toBucketDo(modelBucket)
	}
	return buckets, nil
}

func (r *BucketRepo) UpdateBucket(ctx context.Context, userID int64, name string, update *do.UpdateBucket) (*do.BucketDo, error) {
	qs := query.Use(r.db).Bucket

	updates := map[string]interface{}{}
	if update.Acl != nil {
		updates[qs.Acl.ColumnName().String()] = *update.Acl
	}
	if update.Versioning != nil {
		updates[qs.Versioning.ColumnName().String()] = *update.Versioning
	}
	if update.Status != nil {
		updates[qs.Status.ColumnName().String()] = *update.Status
	}
	if update.StorageClass != "" {
		updates[qs.StorageClass.ColumnName().String()] = update.StorageClass
	}
	if len(updates) == 0 {
		return nil, gorm.ErrInvalidData
	}
	updates[qs.UpdatedAt.ColumnName().String()] = time.Now()

	if _, err := qs.WithContext(ctx).Where(qs.UserID.Eq(userID), qs.Name.Eq(name)).Updates(updates); err != nil {
		return nil, err
	}
	return r.GetByUserAndName(ctx, userID, name)
}

func (r *BucketRepo) DeleteBucket(ctx context.Context, userID int64, name string) error {
	q := query.Use(r.db)
	_, err := q.Bucket.WithContext(ctx).Where(q.Bucket.UserID.Eq(userID), q.Bucket.Name.Eq(name)).Update(q.Bucket.Status, consts.BucketStatusDeleted)
	return err
}

func (r *BucketRepo) UpdateBucketStats(ctx context.Context, userID int64, bucketName string, deltaCount, deltaSize int64) error {
	q := query.Use(r.db)
	_, err := q.Bucket.WithContext(ctx).
		Where(q.Bucket.UserID.Eq(userID), q.Bucket.Name.Eq(bucketName)).
		Updates(map[string]interface{}{
			q.Bucket.ObjectCount.ColumnName().String(): q.Bucket.ObjectCount.Add(deltaCount),
			q.Bucket.StorageSize.ColumnName().String(): q.Bucket.StorageSize.Add(deltaSize),
		})
	return err
}

func (r *BucketRepo) GetByNameWithTx(tx *gorm.DB, ctx context.Context, userID int64, name string) (*do.BucketDo, error) {
	return r.GetByUserAndNameWithTx(tx, ctx, userID, name)
}

func (r *BucketRepo) GetByUserAndNameWithTx(tx *gorm.DB, ctx context.Context, userID int64, name string) (*do.BucketDo, error) {
	q := query.Use(tx)
	modelBucket, err := q.Bucket.WithContext(ctx).Where(q.Bucket.UserID.Eq(userID), q.Bucket.Name.Eq(name)).First()
	if err != nil {
		return nil, err
	}
	return r.toBucketDo(modelBucket), nil
}

func (r *BucketRepo) UpdateBucketStatsWithTx(tx *gorm.DB, ctx context.Context, userID int64, bucketName string, deltaCount, deltaSize int64) error {
	q := query.Use(tx)
	_, err := q.Bucket.WithContext(ctx).
		Where(q.Bucket.UserID.Eq(userID), q.Bucket.Name.Eq(bucketName)).
		Updates(map[string]interface{}{
			q.Bucket.ObjectCount.ColumnName().String(): q.Bucket.ObjectCount.Add(deltaCount),
			q.Bucket.StorageSize.ColumnName().String(): q.Bucket.StorageSize.Add(deltaSize),
		})
	return err
}
