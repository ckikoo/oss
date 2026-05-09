package do

import "time"

type AsyncTaskDo struct {
	ID         int64
	UserId     int64
	TaskID     string
	TaskType   string
	UploadID   string
	ObjectID   int64
	Status     int32
	Progress   int32
	Result     string
	ErrorMsg   string
	RetryCount int32
	MaxRetry   int32
	WorkerID   string
	StartedAt  time.Time
	FinishedAt time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type CreateAsyncTask struct {
	UserId     int64
	TaskID     string
	TaskType   string
	UploadID   string
	ObjectID   int64
	Status     int32
	Progress   int32
	Result     string
	ErrorMsg   string
	RetryCount int32
	MaxRetry   int32
	WorkerID   string
	StartedAt  time.Time
}

type UpdateAsyncTask struct {
	Status     int32
	Progress   int32
	Result     string
	ErrorMsg   string
	RetryCount int32
	WorkerID   string
	StartedAt  time.Time
	FinishedAt time.Time
}
