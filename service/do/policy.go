package do

import "time"

type PolicyPrincipalDo struct {
	Type  string `gorm:"-"`
	Value string `gorm:"-"`
}

type PolicyConditionDo struct {
	Type    string  `gorm:"-"`
	CondKey *string `gorm:"-"`
	Value   string  `gorm:"-"`
}

type CreateBucketPolicy struct {
	Effect     string
	Name       string
	Status     int32
	Principals []*PolicyPrincipalDo
	Actions    []string
	Resources  []string
	Conditions []*PolicyConditionDo
}

type BucketPolicyDo struct {
	ID         int64
	BucketID   int64
	Effect     string
	Name       string
	Status     int32
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Principals []*PolicyPrincipalDo
	Actions    []string
	Resources  []string
	Conditions []*PolicyConditionDo
}
