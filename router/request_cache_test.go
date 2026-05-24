package router

import (
	"context"
	"testing"

	"oss/consts"
	"oss/service/do"

	"github.com/cloudwego/hertz/pkg/app"
)

type countingBucketReader struct {
	bucket *do.BucketDo
	calls  int
}

func (r *countingBucketReader) GetByUserAndName(context.Context, int64, string) (*do.BucketDo, error) {
	r.calls++
	return r.bucket, nil
}

type countingObjectReader struct {
	object *do.ObjectDo
	calls  int
}

func (r *countingObjectReader) GetByKey(context.Context, string, string, string) (*do.ObjectDo, error) {
	r.calls++
	return r.object, nil
}

func TestLoadRouteBucketUsesRequestCache(t *testing.T) {
	var c app.RequestContext
	repo := &countingBucketReader{
		bucket: &do.BucketDo{ID: 10, UserID: 7, Name: "photos"},
	}

	first, err := loadRouteBucket(context.Background(), &c, repo, 7, "photos")
	if err != nil {
		t.Fatalf("first load returned error: %v", err)
	}
	second, err := loadRouteBucket(context.Background(), &c, repo, 7, "photos")
	if err != nil {
		t.Fatalf("second load returned error: %v", err)
	}

	if repo.calls != 1 {
		t.Fatalf("repo calls = %d, want 1", repo.calls)
	}
	if first != repo.bucket || second != repo.bucket {
		t.Fatalf("cached bucket was not reused")
	}
	if value, ok := c.Get(consts.BucketContext); !ok || value != repo.bucket {
		t.Fatalf("bucket context = %v, want cached bucket", value)
	}
}

func TestLoadRouteObjectUsesRequestCache(t *testing.T) {
	var c app.RequestContext
	repo := &countingObjectReader{
		object: &do.ObjectDo{ID: 20, BucketName: "photos", ObjectKey: "a.jpg"},
	}

	first, err := loadRouteObject(context.Background(), &c, repo, "photos", "a.jpg", "")
	if err != nil {
		t.Fatalf("first load returned error: %v", err)
	}
	second, err := loadRouteObject(context.Background(), &c, repo, "photos", "a.jpg", "")
	if err != nil {
		t.Fatalf("second load returned error: %v", err)
	}

	if repo.calls != 1 {
		t.Fatalf("repo calls = %d, want 1", repo.calls)
	}
	if first != repo.object || second != repo.object {
		t.Fatalf("cached object was not reused")
	}
}

func TestGetRouteResourceSupportsCustomKinds(t *testing.T) {
	var c app.RequestContext
	const (
		policyKind routeResourceKind = "policy"
		userKind   routeResourceKind = "user"
	)

	policyCalls := 0
	userCalls := 0

	policy, err := getRouteResource(context.Background(), &c, policyKind, "same-key", func(context.Context) (string, error) {
		policyCalls++
		return "allow", nil
	})
	if err != nil {
		t.Fatalf("policy load returned error: %v", err)
	}
	userID, err := getRouteResource(context.Background(), &c, userKind, "same-key", func(context.Context) (int64, error) {
		userCalls++
		return 42, nil
	})
	if err != nil {
		t.Fatalf("user load returned error: %v", err)
	}
	cachedPolicy, err := getRouteResource(context.Background(), &c, policyKind, "same-key", func(context.Context) (string, error) {
		policyCalls++
		return "deny", nil
	})
	if err != nil {
		t.Fatalf("cached policy load returned error: %v", err)
	}

	if policy != "allow" || cachedPolicy != "allow" || userID != 42 {
		t.Fatalf("unexpected cached values: policy=%q cachedPolicy=%q userID=%d", policy, cachedPolicy, userID)
	}
	if policyCalls != 1 || userCalls != 1 {
		t.Fatalf("loader calls policy=%d user=%d, want 1 and 1", policyCalls, userCalls)
	}
}
