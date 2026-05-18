package do

import "time"

type AsyncTaskDo struct {
	ID          int64
	UserId      int64
	TaskType    string
	BizType     string
	BizID       string
	Status      int32
	Progress    int32
	Result      string
	LastError   string
	RetryCount  int32
	MaxRetry    int32
	LockedBy    string
	LockedUntil time.Time
	StartedAt   time.Time
	FinishedAt  time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateAsyncTask struct {
	UserId      int64
	TaskType    string
	BizType     string
	BizID       string
	Status      int32
	Progress    int32
	Result      string
	LastError   string
	RetryCount  int32
	MaxRetry    int32
	LockedBy    string
	LockedUntil time.Time
	StartedAt   time.Time
}

type UpdateAsyncTask struct {
	Status      int32
	Progress    int32
	Result      string
	LastError   string
	RetryCount  int32
	LockedBy    string
	LockedUntil time.Time
	StartedAt   time.Time
	FinishedAt  time.Time
}
