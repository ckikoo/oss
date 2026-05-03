package dto

type PolicyPrincipalItem struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type PolicyConditionItem struct {
	Type    string  `json:"type"`
	CondKey *string `json:"cond_key,omitempty"`
	Value   string  `json:"value"`
}

type CreateBucketPolicyReq struct {
	Name        string                `json:"name"`
	Effect      string                `json:"effect,omitempty"`
	Principals  []PolicyPrincipalItem `json:"principals"`
	Actions     []string              `json:"actions"`
	Resources   []string              `json:"resources"`
	Conditions  []PolicyConditionItem `json:"conditions,omitempty"`
	Description string                `json:"description,omitempty"`
}

type CreateBucketPolicyResp struct {
	PolicyID  int64  `json:"policy_id"`
	Name      string `json:"name"`
	Status    int32  `json:"status"`
	BucketID  int64  `json:"bucket_id"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type BucketPolicyItem struct {
	PolicyID   int64                 `json:"policy_id"`
	BucketID   int64                 `json:"bucket_id"`
	Effect     string                `json:"effect"`
	Name       string                `json:"name"`
	Status     int32                 `json:"status"`
	Principals []PolicyPrincipalItem `json:"principals"`
	Actions    []string              `json:"actions"`
	Resources  []string              `json:"resources"`
	Conditions []PolicyConditionItem `json:"conditions,omitempty"`
	CreatedAt  int64                 `json:"created_at"`
	UpdatedAt  int64                 `json:"updated_at"`
}

type ListBucketPoliciesResp struct {
	Items []*BucketPolicyItem `json:"items"`
}
