package bucket

import (
	"context"
	"time"

	"oss/adaptor"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/consts"
	"oss/service/do"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type BucketRepo struct {
	db *gorm.DB
}

var _ IBucketRepo = (*BucketRepo)(nil)

func NewBucketRepo(adaptor adaptor.IAdaptor) *BucketRepo {
	sqlDB := adaptor.GetDB()
	ormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	return &BucketRepo{db: ormDB}
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

	r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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

	return modelBucket.ID, nil
}

func (r *BucketRepo) GetByName(ctx context.Context, name string) (*do.BucketDo, error) {
	q := query.Use(r.db)
	modelBucket, err := q.Bucket.WithContext(ctx).Where(q.Bucket.Name.Eq(name)).First()
	if err != nil {
		return nil, err
	}
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
	}, nil
}

func (r *BucketRepo) GetByID(ctx context.Context, id int64) (*do.BucketDo, error) {
	q := query.Use(r.db)
	modelBucket, err := q.Bucket.WithContext(ctx).Where(q.Bucket.ID.Eq(id)).First()
	if err != nil {
		return nil, err
	}
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
	}, nil
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
		buckets[i] = &do.BucketDo{
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
	return buckets, nil
}

func (r *BucketRepo) UpdateBucket(ctx context.Context, name string, update *do.UpdateBucket) (*do.BucketDo, error) {
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

	if _, err := qs.WithContext(ctx).Where(qs.Name.Eq(name)).Updates(updates); err != nil {
		return nil, err
	}
	return r.GetByName(ctx, name)
}

func (r *BucketRepo) DeleteBucket(ctx context.Context, name string) error {
	q := query.Use(r.db)
	_, err := q.Bucket.WithContext(ctx).Where(q.Bucket.Name.Eq(name)).Update(q.Bucket.Status, consts.BucketStatusDeleted)
	return err
}

func (r *BucketRepo) UpdateBucketStats(ctx context.Context, bucketName string, deltaCount, deltaSize int64) error {
	q := query.Use(r.db)
	_, err := q.Bucket.WithContext(ctx).
		Where(q.Bucket.Name.Eq(bucketName)).
		Updates(map[string]interface{}{
			q.Bucket.ObjectCount.ColumnName().String(): q.Bucket.ObjectCount.Add(deltaCount),
			q.Bucket.StorageSize.ColumnName().String(): q.Bucket.StorageSize.Add(deltaSize),
		})
	return err
}
