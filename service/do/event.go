package do

import "time"

// EventRuleDo 事件规则领域对象
type EventRuleDo struct {
	ID         int64     `json:"id"`
	BucketID   int64     `json:"bucket_id"`
	RuleName   string    `json:"rule_name"`
	Events     string    `json:"events"` // 逗号分隔的事件类型
	Prefix     *string   `json:"prefix"`
	Suffix     *string   `json:"suffix"`
	TargetType string    `json:"target_type"`
	TargetURL  *string   `json:"target_url"`
	Secret     *string   `json:"secret"`
	Status     int32     `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// CreateEventRule 创建事件规则请求
type CreateEventRule struct {
	BucketID   int64    `json:"bucket_id"`
	RuleName   string   `json:"rule_name"`
	Events     []string `json:"events"`
	Prefix     *string  `json:"prefix"`
	Suffix     *string  `json:"suffix"`
	TargetType string   `json:"target_type"`
	TargetURL  *string  `json:"target_url"`
	Secret     *string  `json:"secret"`
}

// UpdateEventRule 更新事件规则请求
type UpdateEventRule struct {
	RuleName   *string `json:"rule_name"`
	Events     *string `json:"events"`
	Prefix     *string `json:"prefix"`
	Suffix     *string `json:"suffix"`
	TargetType *string `json:"target_type"`
	TargetURL  *string `json:"target_url"`
	Secret     *string `json:"secret"`
	Status     *int32  `json:"status"`
}

// EventDeliveryDo 事件投递领域对象
type EventDeliveryDo struct {
	ID           int64      `json:"id"`
	RuleID       int64      `json:"rule_id"`
	EventType    string     `json:"event_type"`
	ObjectKey    *string    `json:"object_key"`
	Payload      string     `json:"payload"`
	Status       int32      `json:"status"`
	RetryCount   int32      `json:"retry_count"`
	ResponseCode *int32     `json:"response_code"`
	ResponseBody *string    `json:"response_body"`
	NextRetryAt  *time.Time `json:"next_retry_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// CreateEventDelivery 创建事件投递请求
type CreateEventDelivery struct {
	RuleID    int64   `json:"rule_id"`
	EventType string  `json:"event_type"`
	ObjectKey *string `json:"object_key"`
	Payload   string  `json:"payload"`
	Status    int32   `json:"status"`
}

// UpdateEventDelivery 更新事件投递请求
type UpdateEventDelivery struct {
	Status       *int32     `json:"status"`
	RetryCount   *int32     `json:"retry_count"`
	ResponseCode *int32     `json:"response_code"`
	ResponseBody *string    `json:"response_body"`
	NextRetryAt  *time.Time `json:"next_retry_at"`
}
