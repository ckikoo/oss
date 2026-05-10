package dto

import "time"

// CreateEventRuleReq 创建事件规则请求
type CreateEventRuleReq struct {
	RuleName   string `json:"rule_name" binding:"required"`
	BucketName string
	Events     []string `json:"events" binding:"required,min=1"`
	Prefix     *string  `json:"prefix,omitempty"`
	Suffix     *string  `json:"suffix,omitempty"`
	TargetType string   `json:"target_type" binding:"required"`
	TargetURL  *string  `json:"target_url,omitempty"`
	Secret     *string  `json:"secret,omitempty"`
}

// CreateEventRuleResp 创建事件规则响应
type CreateEventRuleResp struct {
	RuleID int64 `json:"rule_id"`
}

// ListEventRulesResp 列出事件规则响应
type ListEventRulesResp struct {
	Rules []*EventRuleInfo `json:"rules"`
}

// EventRuleInfo 事件规则信息
type EventRuleInfo struct {
	RuleID     int64     `json:"rule_id"`
	RuleName   string    `json:"rule_name"`
	Events     []string  `json:"events"`
	Prefix     *string   `json:"prefix,omitempty"`
	Suffix     *string   `json:"suffix,omitempty"`
	TargetType string    `json:"target_type"`
	TargetURL  *string   `json:"target_url,omitempty"`
	Status     int32     `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// UpdateEventRuleReq 更新事件规则请求
type UpdateEventRuleReq struct {
	RuleName   *string   `json:"rule_name,omitempty"`
	Events     *[]string `json:"events,omitempty"`
	Prefix     *string   `json:"prefix,omitempty"`
	Suffix     *string   `json:"suffix,omitempty"`
	TargetType *string   `json:"target_type,omitempty"`
	TargetURL  *string   `json:"target_url,omitempty"`
	Secret     *string   `json:"secret,omitempty"`
	Status     *int32    `json:"status,omitempty"`
}
