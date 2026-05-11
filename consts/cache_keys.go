package consts

import "fmt"

// Cache key prefixes and TTLs
const (
	// Bucket cache
	CacheKeyBucketByName = "oss:bucket:name:%d:%s" // userID:name
	CacheKeyBucketByID   = "oss:bucket:id:%d"      // bucketID

	// AccessKey cache
	CacheKeyAccessKey = "oss:accesskey:%s" // accessKey string

	// Object cache (use cautiously due to volume)
	CacheKeyObjectByKey = "oss:object:%s:%s:%s" // bucket:objectKey:versionID

	// Cache TTLs (seconds)
	CacheTTLBucket    = 3600 // 1 hour for bucket data
	CacheTTLAccessKey = 1800 // 30 minutes for access key
	CacheTTLObject    = 300  // 5 minutes for object metadata
	CacheTTLList      = 600  // 10 minutes for list results
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

// Object cache key generator
func ObjectCacheKey(bucketName, objectKey, versionID string) string {
	return fmt.Sprintf(CacheKeyObjectByKey, bucketName, objectKey, versionID)
}
