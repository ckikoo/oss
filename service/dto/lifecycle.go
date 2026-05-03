package dto

type CreateLifecycleRuleReq struct {
	RuleName               string  `json:"rule_name" form:"rule_name" validate:"required"`
	Prefix                 *string `json:"prefix" form:"prefix"`
	TransitionDays         *int32  `json:"transition_days" form:"transition_days"`
	TransitionStorageClass *string `json:"transition_storage_class" form:"transition_storage_class"`
	ExpirationDays         *int32  `json:"expiration_days" form:"expiration_days"`
	Status                 *int32  `json:"status" form:"status"`
}

type UpdateLifecycleRuleReq struct {
	RuleName               *string `json:"rule_name" form:"rule_name"`
	Prefix                 *string `json:"prefix" form:"prefix"`
	TransitionDays         *int32  `json:"transition_days" form:"transition_days"`
	TransitionStorageClass *string `json:"transition_storage_class" form:"transition_storage_class"`
	ExpirationDays         *int32  `json:"expiration_days" form:"expiration_days"`
	Status                 *int32  `json:"status" form:"status"`
}

type LifecycleRuleItem struct {
	RuleID                 int64   `json:"rule_id"`
	RuleName               string  `json:"rule_name"`
	Prefix                 *string `json:"prefix"`
	TransitionDays         *int32  `json:"transition_days"`
	TransitionStorageClass *string `json:"transition_storage_class"`
	ExpirationDays         *int32  `json:"expiration_days"`
	Status                 int32   `json:"status"`
	CreatedAt              int64   `json:"created_at"`
	UpdatedAt              int64   `json:"updated_at"`
}

type CreateLifecycleRuleResp struct {
	RuleID                 int64   `json:"rule_id"`
	RuleName               string  `json:"rule_name"`
	Prefix                 *string `json:"prefix"`
	TransitionDays         *int32  `json:"transition_days"`
	TransitionStorageClass *string `json:"transition_storage_class"`
	ExpirationDays         *int32  `json:"expiration_days"`
	Status                 int32   `json:"status"`
	CreatedAt              int64   `json:"created_at"`
	UpdatedAt              int64   `json:"updated_at"`
}

type ListLifecycleRulesResp struct {
	Items []*LifecycleRuleItem `json:"items"`
}
