package do

import "time"

type CreateLifecycleRule struct {
	BucketID                        int64
	RuleName                        string
	Status                          int32
	Prefix                          *string
	TransitionDays                  *int32
	TransitionStorageClass          *string
	ExpirationDays                  *int32
	NoncurrentVersionExpirationDays *int32
	AbortIncompleteMultipartDays    *int32
}

type UpdateLifecycleRule struct {
	RuleName                        *string
	Status                          *int32
	Prefix                          *string
	TransitionDays                  *int32
	TransitionStorageClass          *string
	ExpirationDays                  *int32
	NoncurrentVersionExpirationDays *int32
	AbortIncompleteMultipartDays    *int32
}

type LifecycleRuleDo struct {
	ID                              int64
	BucketID                        int64
	RuleName                        string
	Status                          int32
	Prefix                          *string
	TransitionDays                  *int32
	TransitionStorageClass          *string
	ExpirationDays                  *int32
	NoncurrentVersionExpirationDays *int32
	AbortIncompleteMultipartDays    int32
	CreatedAt                       time.Time
	UpdatedAt                       time.Time
}
