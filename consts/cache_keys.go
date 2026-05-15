package consts

import (
	"fmt"
	"net/url"
	"strings"
)

// Cache key prefixes and TTLs
const (
	// Bucket cache
	CacheKeyBucketByName = "oss:bucket:name:%d:%s" // userID:name
	CacheKeyBucketByID   = "oss:bucket:id:%d"      // bucketID

	// AccessKey cache
	CacheKeyAccessKey = "oss:accesskey:%s" // accessKey string

	// Bucket CORS cache
	CacheKeyBucketCorsByOrigin = "oss:bucket:cors:%d:%s:%s" // userID:bucketName:origin

	// Object cache (use cautiously due to volume)
	CacheKeyObjectByKey         = "oss:object:%s:%s:%s"     // bucket:objectKey:versionID
	CacheKeyObjectLatestVersion = "oss:object:latest:%s:%s" // bucket:objectKey
	CacheKeyUserByID            = "oss:user:id:%d"          // userID

	// Cache TTLs (seconds)
	CacheTTLBucket     = 3600 // 1 hour for bucket data
	CacheTTLAccessKey  = 1800 // 30 minutes for access key
	CacheTTLBucketCors = 300  // 5 minutes for bucket CORS rules
	CacheTTLObject     = 300  // 5 minutes for object metadata
	CacheTTLUser       = 3600 // 1 hour for user data
)

// Bucket cache key generators
func BucketCacheKeyByName(userID int64, bucketName string) string {
	return fmt.Sprintf(CacheKeyBucketByName, userID, bucketName)
}

func BucketCacheKeyByID(bucketID int64) string {
	return fmt.Sprintf(CacheKeyBucketByID, bucketID)
}

// AccessKey cache key generator
func AccessKeyCacheKey(accessKey string) string {
	return fmt.Sprintf(CacheKeyAccessKey, accessKey)
}

func BucketCorsOriginCacheKey(userID int64, bucketName, origin string) string {
	normalizedOrigin := url.QueryEscape(strings.ToLower(strings.TrimSpace(origin)))
	return fmt.Sprintf(CacheKeyBucketCorsByOrigin, userID, bucketName, normalizedOrigin)
}

// Object cache key generator
func ObjectCacheKey(bucketName, objectKey, versionID string) string {
	return fmt.Sprintf(CacheKeyObjectByKey, bucketName, objectKey, versionID)
}

func ObjectLatestVersionCacheKey(bucketName, objectKey string) string {
	return fmt.Sprintf(CacheKeyObjectLatestVersion, bucketName, objectKey)
}

func UserCacheKeyByID(userID int64) string {
	return fmt.Sprintf(CacheKeyUserByID, userID)
}
