package do

import "time"

type PolicyPrincipalDo struct {
	Type  string
	Value string
}

type PolicyConditionDo struct {
	Type    string
	CondKey string
	Value   string
}

type EvaluateReq struct {
	BucketID   int64
	Principals []string // ["user:42", "ak:AKXXXXXXXX"]
	Action     string   // "GetObject"
	Resource   string   // "arn:oss:::my-bucket/dir/file.jpg"
	SourceIP   string
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
