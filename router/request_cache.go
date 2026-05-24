package router

import (
	"context"
	"fmt"
	"reflect"

	"oss/common"
	"oss/consts"
	"oss/service/do"

	"github.com/cloudwego/hertz/pkg/app"
)

const routeResourceCacheContext = "_route_resource_cache"

type routeResourceKind string

const (
	routeResourceBucket routeResourceKind = "bucket"
	routeResourceObject routeResourceKind = "object"
)

type bucketReader interface {
	GetByUserAndName(ctx context.Context, userID int64, name string) (*do.BucketDo, error)
}

type objectReader interface {
	GetByKey(ctx context.Context, bucketName, objectKey, versionID string) (*do.ObjectDo, error)
}

type routeResourceLoader[T any] func(context.Context) (T, error)

type routeResourceCacheKey struct {
	kind routeResourceKind
	key  string
}

type routeResourceCache struct {
	values map[routeResourceCacheKey]any
}

func getRouteResourceCache(c *app.RequestContext) *routeResourceCache {
	if value, ok := c.Get(routeResourceCacheContext); ok {
		if rc, ok := value.(*routeResourceCache); ok && rc != nil {
			return rc
		}
	}

	rc := &routeResourceCache{
		values: make(map[routeResourceCacheKey]any),
	}
	c.Set(routeResourceCacheContext, rc)
	return rc
}

func authenticatedUserID(c *app.RequestContext) int64 {
	userID := c.GetInt64(consts.UserKeyContext)
	if userID != 0 {
		return userID
	}

	if info, ok := c.Get(consts.UserInfoContext); ok {
		if userInfo, ok := info.(*common.UserInfoCtx); ok && userInfo != nil {
			return userInfo.UserID
		}
	}
	return 0
}

func tokenGranted(c *app.RequestContext) bool {
	value, ok := c.Get(consts.TokenGranted)
	if !ok {
		return false
	}
	granted, ok := value.(bool)
	return ok && granted
}

func getRouteResource[T any](
	ctx context.Context,
	c *app.RequestContext,
	kind routeResourceKind,
	key string,
	load routeResourceLoader[T],
) (T, error) {
	var zero T
	if c == nil || kind == "" || key == "" || load == nil {
		return zero, nil
	}

	cacheKey := routeResourceCacheKey{kind: kind, key: key}
	rc := getRouteResourceCache(c)
	if value, ok := rc.values[cacheKey]; ok {
		cached, ok := value.(T)
		if ok {
			return cached, nil
		}
		delete(rc.values, cacheKey)
	}

	value, err := load(ctx)
	if err != nil {
		return zero, err
	}
	setRouteResource(c, kind, key, value)
	return value, nil
}

func setRouteResource[T any](c *app.RequestContext, kind routeResourceKind, key string, value T) {
	if c == nil || kind == "" || key == "" || !cacheableRouteResource(value) {
		return
	}
	getRouteResourceCache(c).values[routeResourceCacheKey{kind: kind, key: key}] = value
}

func loadRouteBucket(ctx context.Context, c *app.RequestContext, repo bucketReader, userID int64, bucketName string) (*do.BucketDo, error) {
	if repo == nil || userID == 0 || bucketName == "" {
		return nil, nil
	}

	key := bucketRequestCacheKey(userID, bucketName)
	if bucket := bucketFromContext(c, userID, bucketName); bucket != nil {
		setRouteResource(c, routeResourceBucket, key, bucket)
		return bucket, nil
	}

	bucket, err := getRouteResource(ctx, c, routeResourceBucket, key, func(ctx context.Context) (*do.BucketDo, error) {
		return repo.GetByUserAndName(ctx, userID, bucketName)
	})
	if err != nil {
		return nil, err
	}
	if bucket != nil {
		c.Set(consts.BucketContext, bucket)
	}
	return bucket, nil
}

func loadRouteObject(ctx context.Context, c *app.RequestContext, repo objectReader, bucketName, objectKey, versionID string) (*do.ObjectDo, error) {
	if repo == nil || bucketName == "" || objectKey == "" {
		return nil, nil
	}

	key := objectRequestCacheKey(bucketName, objectKey, versionID)
	return getRouteResource(ctx, c, routeResourceObject, key, func(ctx context.Context) (*do.ObjectDo, error) {
		return repo.GetByKey(ctx, bucketName, objectKey, versionID)
	})
}

func bucketFromContext(c *app.RequestContext, userID int64, bucketName string) *do.BucketDo {
	if c == nil {
		return nil
	}
	value, ok := c.Get(consts.BucketContext)
	if !ok {
		return nil
	}
	bucket, ok := value.(*do.BucketDo)
	if !ok || bucket == nil || bucket.UserID != userID || bucket.Name != bucketName {
		return nil
	}
	return bucket
}

func cacheableRouteResource(value any) bool {
	if value == nil {
		return false
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !v.IsNil()
	default:
		return true
	}
}

func bucketRequestCacheKey(userID int64, bucketName string) string {
	return fmt.Sprintf("%d\x00%s", userID, bucketName)
}

func objectRequestCacheKey(bucketName, objectKey, versionID string) string {
	return bucketName + "\x00" + objectKey + "\x00" + versionID
}
