package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/common"
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"
	videoSvc "oss/service/video"
	"oss/utils/ip"
	"oss/utils/tools"

	"go.uber.org/zap"
)

func (srv *Service) PutObjectStream(ctx *common.UserInfoCtx, req *dto.PutObjectStreamReq, body io.Reader) (*dto.PutObjectResp, common.Errno) {
	if req.CallbackUrl != "" {
		if err := ip.ValidateCallbackURL(req.CallbackUrl); err != nil {
			return nil, common.ParamErr.WithErr(err)
		}
	}

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, req.BucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if bucket == nil {
		return nil, common.BucketNotFoundErr
	}

	objectKeyHash := tools.Md5Hash(req.ObjectKey)
	oldObject, err := srv.objRepo.GetByKey(ctx, req.BucketName, req.ObjectKey, "")
	if err != nil && !errors.Is(err, repoerr.ErrNotFound) {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if errors.Is(err, repoerr.ErrNotFound) {
		oldObject = nil
	}

	versionID := tools.UUIDHex()
	if bucket.Versioning == consts.BucketVersioningDisabled && objectVisible(oldObject) && !req.Overwrite {
		return nil, common.FileNameExists
	}

	rules, err := srv.lifecycleRepo.ListLifecycleRules(ctx, bucket.ID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	uInfo, err := srv.userRepo.GetUserInfoById(ctx, ctx.UserID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	expectedStorageDelta := req.ContentLength
	if expectedStorageDelta < 0 {
		expectedStorageDelta = 0
	}
	if bucket.Versioning == consts.BucketVersioningDisabled && objectVisible(oldObject) {
		expectedStorageDelta -= oldObject.Size
	}
	if uInfo.StorageQuota != 0 && expectedStorageDelta > 0 && uInfo.StorageUsed+expectedStorageDelta > uInfo.StorageQuota {
		return nil, common.StorageQuotaOver
	}

	putResult, err := srv.storage.Put(ctx, req.BucketName, req.ObjectKey, versionID, body)
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}

	storageClass := req.StorageClass
	if storageClass == "" {
		storageClass = consts.StorageClassStandard
	}

	createObj := &do.CreateObject{
		BucketID:      bucket.ID,
		BucketName:    req.BucketName,
		ObjectKey:     req.ObjectKey,
		ObjectKeyHash: objectKeyHash,
		VersionID:     versionID,
		Size:          putResult.Size,
		Etag:          putResult.Etag,
		ContentType:   &req.ContentType,
		StorageClass:  storageClass,
		IsMultipart:   consts.ObjectIsMultipartNormal,
		StoragePath:   &putResult.StoragePath,
		Acl:           req.Acl,
		UploadID:      stringPtrIfNotEmpty(req.UploadID),
		Metadata:      stringPtrIfNotEmpty(req.Metadata),
	}

	var objectID int64
	var oldObjectToCleanup *do.ObjectDo
	deltaCount := int64(0)
	deltaSize := putResult.Size
	if !objectVisible(oldObject) {
		deltaCount = 1
	}
	if bucket.Versioning == consts.BucketVersioningDisabled && objectVisible(oldObject) {
		oldObjectToCleanup = oldObject
		deltaSize = putResult.Size - oldObject.Size
	}

	videoCleanupPlan, err := srv.videoCleanup.PlanObjectVersionCleanup(ctx, oldObjectToCleanup)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if videoCleanupPlan != nil && videoCleanupPlan.DerivedSize > 0 {
		deltaSize -= videoCleanupPlan.DerivedSize
	}

	err = srv.txManger.RunInTx(ctx, func(ctx1 context.Context, tx tx.Tx) error {
		objRepo := srv.objRepo.WithTx(tx)
		if err := objRepo.MarkAllNotLatest(ctx1, createObj.BucketName, createObj.ObjectKey); err != nil {
			return err
		}

		if oldObjectToCleanup != nil {
			if _, err := objRepo.MarkVersionPurged(ctx1, oldObjectToCleanup.BucketName, oldObjectToCleanup.ObjectKey, oldObjectToCleanup.VersionID); err != nil {
				return err
			}
			if oldObjectToCleanup.IsMultipart == consts.ObjectIsMultipartMerged && oldObjectToCleanup.UploadID != nil {
				if _, err := srv.asyncRepo.WithTx(tx).FailAsyncTasksByBiz(ctx1, ctx.UserID, consts.TaskTypePhysicalMerge, consts.TaskBizTypeUpload, []string{*oldObjectToCleanup.UploadID}, "object already deleted"); err != nil {
					return err
				}
				if err := srv.multipartRepo.WithTx(tx).DeleteMultipartParts(ctx1, ctx.UserID, *oldObjectToCleanup.UploadID); err != nil {
					return err
				}
			}
			if err := srv.videoCleanup.MarkDeletedInTx(ctx1, tx, videoCleanupPlan); err != nil {
				return err
			}
		}

		objectID, err = objRepo.CreateObject(ctx1, createObj)
		if err != nil {
			return err
		}

		if deltaCount != 0 || deltaSize != 0 {
			if err := srv.bucketRepo.WithTx(tx).UpdateBucketStats(ctx1, ctx.UserID, req.BucketName, deltaCount, deltaSize); err != nil {
				return err
			}
		}
		if deltaSize != 0 {
			if err := srv.userRepo.WithTx(tx).UpdateStorageUsed(ctx1, ctx.UserID, deltaSize); err != nil {
				return err
			}
		}

		return srv.meteringRepo.WithTx(tx).UpdateDailyMetrics(ctx1, bucket.UserID, &bucket.ID, time.Now(), deltaSize, deltaCount, putResult.Size, 0, 0, 1, 0)
	})
	if err != nil {
		if deleteErr := srv.storage.Delete(ctx, putResult.StoragePath); deleteErr != nil {
			srv.logger.Warn("failed to cleanup object storage after PutObjectStream transaction failure",
				zap.String("storage_path", putResult.StoragePath),
				zap.Error(deleteErr))
		}
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if oldObjectToCleanup != nil {
		srv.deleteObjectStorage(ctx, oldObjectToCleanup)
		srv.videoCleanup.AfterCommit(ctx, videoCleanupPlan)
	}

	srv.scheduleVideoTranscode(ctx, &videoSvc.TranscodeSource{
		UserID:        ctx.UserID,
		BucketID:      bucket.ID,
		BucketName:    req.BucketName,
		ObjectID:      objectID,
		ObjectKey:     req.ObjectKey,
		ObjectKeyHash: objectKeyHash,
		VersionID:     versionID,
		SourceEtag:    putResult.Etag,
		SourceSize:    putResult.Size,
		ContentType:   req.ContentType,
		SourcePath:    putResult.StoragePath,
	})

	now := time.Now()
	for _, rule := range rules {
		prefix := ""
		if rule.Prefix != nil {
			prefix = *rule.Prefix
		}
		if strings.HasPrefix(req.ObjectKey, prefix) {
			srv.scheduleObjectEvents(ctx, bucket.ID, rule, prefix, &do.ObjectDo{
				ObjectKey: req.ObjectKey,
				CreatedAt: now,
				VersionID: createObj.VersionID,
			}, now)
		}
	}

	go srv.eventService.TriggerEvent(ctx, bucket.ID, consts.EventTypePutObject, req.ObjectKey, map[string]interface{}{
		"bucket_name":   req.BucketName,
		"object_key":    req.ObjectKey,
		"size":          putResult.Size,
		"etag":          putResult.Etag,
		"content_type":  req.ContentType,
		"storage_class": storageClass,
	})

	return &dto.PutObjectResp{
		ObjectKey:   req.ObjectKey,
		Size:        putResult.Size,
		Etag:        putResult.Etag,
		StoragePath: putResult.StoragePath,
		VersionID:   versionID,
	}, common.OK
}

func (srv *Service) GetObjectStream(ctx *common.UserInfoCtx, bucketName, objectKey, versionID string) (*dto.ObjectStreamResp, common.Errno) {
	obj, err := srv.objRepo.GetByKey(ctx, bucketName, objectKey, versionID)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}
	if !objectVisible(obj) {
		return nil, common.ResouceNotFoundErr
	}

	contentType := "application/octet-stream"
	if obj.ContentType != nil && *obj.ContentType != "" {
		contentType = *obj.ContentType
	}

	var body io.ReadCloser
	if obj.IsMultipart == consts.ObjectIsMultipartMerged {
		body, err = srv.openMultipartObject(ctx, obj)
	} else {
		if obj.StoragePath == nil {
			return nil, common.ServerErr.WithMsg("storage path not found")
		}
		body, err = srv.storage.Get(ctx, *obj.StoragePath)
	}
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}

	body = &meteredObjectReadCloser{
		ReadCloser: body,
		onClose: func(n int64) {
			if errno := srv.incrementGetObjectMetering(ctx, obj, n); errno.NotOk() {
				srv.logger.Warn("failed to update get object metering",
					zap.String("bucket_name", bucketName),
					zap.String("object_key", objectKey),
					zap.String("version_id", obj.VersionID),
					zap.Int64("transmitted", n))
			}
			go srv.eventService.TriggerEvent(ctx, obj.BucketID, consts.EventTypeGetObject, objectKey, map[string]interface{}{
				"bucket_name": bucketName,
				"object_key":  objectKey,
				"size":        obj.Size,
				"etag":        obj.Etag,
				"transmitted": n,
			})
		},
	}

	return &dto.ObjectStreamResp{
		ObjectKey:     obj.ObjectKey,
		Size:          obj.Size,
		Etag:          obj.Etag,
		ContentType:   contentType,
		StorageClass:  obj.StorageClass,
		VersionID:     obj.VersionID,
		LastModified:  obj.UpdatedAt.UnixMilli(),
		Body:          body,
		IsMultipart:   obj.IsMultipart == consts.ObjectIsMultipartMerged,
		ContentLength: obj.Size,
	}, common.OK
}

func (srv *Service) openMultipartObject(ctx *common.UserInfoCtx, obj *do.ObjectDo) (io.ReadCloser, error) {
	if obj.UploadID == nil {
		return nil, fmt.Errorf("upload_id not found for multipart object")
	}

	parts, err := srv.multipartRepo.ListMultipartParts(ctx, ctx.UserID, *obj.UploadID)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("no parts found for multipart object")
	}

	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})
	for i := int32(1); i <= int32(len(parts)); i++ {
		if parts[i-1].PartNumber != i {
			return nil, fmt.Errorf("multipart parts are not continuous")
		}
	}

	readers := make([]io.Reader, 0, len(parts))
	closers := make([]io.Closer, 0, len(parts))
	for _, part := range parts {
		file, err := srv.storage.Get(ctx, part.StoragePath)
		if err != nil {
			for _, closer := range closers {
				if closeErr := closer.Close(); closeErr != nil {
					srv.logger.Warn("failed to close multipart part after open error", zap.Error(closeErr))
				}
			}
			return nil, err
		}
		readers = append(readers, file)
		closers = append(closers, file)
	}

	return &multiReadCloser{
		Reader:  io.MultiReader(readers...),
		closers: closers,
	}, nil
}

type multiReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (rc *multiReadCloser) Close() error {
	var firstErr error
	for _, closer := range rc.closers {
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type meteredObjectReadCloser struct {
	io.ReadCloser
	count   int64
	closed  bool
	onClose func(int64)
}

func (rc *meteredObjectReadCloser) Read(p []byte) (int, error) {
	n, err := rc.ReadCloser.Read(p)
	rc.count += int64(n)
	if err == io.EOF {
		_ = rc.Close()
	}
	return n, err
}

func (rc *meteredObjectReadCloser) Close() error {
	if rc.closed {
		return nil
	}
	rc.closed = true
	err := rc.ReadCloser.Close()
	if rc.onClose != nil {
		rc.onClose(rc.count)
	}
	return err
}

func stringPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
