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

	// Event cache
	CacheKeyEventRulesByBucket       = "oss:event:bucket:%d:rules"        // bucketID
	CacheKeyEventActiveRulesByBucket = "oss:event:bucket:%d:active_rules" // bucketID
	CacheKeyEventRuleByID            = "oss:event:rule:%d"                // ruleID

	// Object cache (use cautiously due to volume)
	CacheKeyObjectByKey         = "oss:object:%s:%s:%s"     // bucket:objectKey:versionID
	CacheKeyObjectLatestVersion = "oss:object:latest:%s:%s" // bucket:objectKey
	CacheKeyUserByID            = "oss:user:id:%d"          // userID

	// Video cache
	CacheKeyVideoTranscodeByID     = "oss:video:transcode:id:%d"
	CacheKeyVideoTranscodeByObjVer = "oss:video:transcode:obj:%d:%s"

	CacheKeyVideoProfilesByTranscode = "oss:video:profiles:%d"
	CacheKeyVideoDoneProfilesByTC    = "oss:video:profiles:done:%d"
	CacheKeyVideoProfileByID         = "oss:video:profile:id:%d"

	CacheKeyVideoEncryptKeyByKeyID     = "oss:video:enckey:keyid:%s"
	CacheKeyVideoEncryptKeyByProfileID = "oss:video:enckey:profile:%d"

	// Cache TTLs (seconds)
	CacheTTLBucket          = 3600 // 1 hour for bucket data
	CacheTTLAccessKey       = 1800 // 30 minutes for access key
	CacheTTLBucketCors      = 300  // 5 minutes for bucket CORS rules
	CacheTTLEventRule       = 300  // 5 minutes for event rule data
	CacheTTLObject          = 300  // 5 minutes for object metadata
	CacheTTLUser            = 3600 // 1 hour for user data
	CacheTTLVideoTranscode  = 600  // 10 minutes for video transcode state
	CacheTTLVideoProfile    = 300  // 5 minutes for video profile state
	CacheTTLVideoEncryptKey = 3600 // 1 hour for HLS encrypt keys
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

func EventRulesCacheKey(bucketID int64) string {
	return fmt.Sprintf(CacheKeyEventRulesByBucket, bucketID)
}

func EventActiveRulesCacheKey(bucketID int64) string {
	return fmt.Sprintf(CacheKeyEventActiveRulesByBucket, bucketID)
}

func EventRuleCacheKey(ruleID int64) string {
	return fmt.Sprintf(CacheKeyEventRuleByID, ruleID)
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

func VideoTranscodeCacheKey(transcodeID int64) string {
	return fmt.Sprintf(CacheKeyVideoTranscodeByID, transcodeID)
}

func VideoTranscodeByObjectVersionCacheKey(objectID int64, versionID string) string {
	return fmt.Sprintf(CacheKeyVideoTranscodeByObjVer, objectID, versionID)
}

func VideoProfilesCacheKey(transcodeID int64) string {
	return fmt.Sprintf(CacheKeyVideoProfilesByTranscode, transcodeID)
}

func VideoDoneProfilesCacheKey(transcodeID int64) string {
	return fmt.Sprintf(CacheKeyVideoDoneProfilesByTC, transcodeID)
}

func VideoProfileCacheKey(profileID int64) string {
	return fmt.Sprintf(CacheKeyVideoProfileByID, profileID)
}

func VideoEncryptKeyByKeyIDCacheKey(keyID string) string {
	return fmt.Sprintf(CacheKeyVideoEncryptKeyByKeyID, keyID)
}

func VideoEncryptKeyByProfileIDCacheKey(profileID int64) string {
	return fmt.Sprintf(CacheKeyVideoEncryptKeyByProfileID, profileID)
}
