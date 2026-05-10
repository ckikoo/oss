package bucket

import (
	"gorm.io/gorm"
)

type BucketRepo struct {
	db *gorm.DB
}
