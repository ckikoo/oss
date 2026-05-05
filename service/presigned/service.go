package presigned

// type Service struct {
// 	bucketRepo bucketRepo.IBucketRepo
// }

// func NewService(adaptor adaptor.IAdaptor) *Service {
// 	return &Service{
// 		bucketRepo: bucketRepo.NewBucketRepo(adaptor),
// 	}
// }

// func (srv *Service) CreatePresignedUrl(ctx context.Context, ak string, sk string, req *dto.CreatePresignedUrlReq) (*dto.CreatePresignedUrlResp, common.Errno) {

// 	bucket, err := srv.bucketRepo.GetByName(ctx, req.BucketName)
// 	if err != nil {
// 		return nil, common.ParamErr.WithErr(err)
// 	}

// 	var expiresAt time.Time
// 	// 设置了失效时间
// 	if req.ExpiresIn > 0 {
// 		expiresAt = time.Now().Add(time.Duration(req.ExpiresIn) * time.Second)
// 	}

// 	url := CreatePresignedURL(ak, sk, bucket.Region, fmt.Sprintf("%s.%s", bucket.Name, consts.ServerName), bucket.Name, req.ObjectKey, "GET", req.ExpiresIn)

// 	return &dto.CreatePresignedUrlResp{
// 		URL:       url,
// 		ExpiresAt: expiresAt.UnixMilli(),
// 	}, common.OK
// }

// func (srv *Service) CreateSimpleUploadURL(ctx context.Context, ak string, sk string, req *dto.CreateDownloadURLReq) (*dto.CreatePresignedUrlResp, common.Errno) {
// 	bucket, err := srv.bucketRepo.GetByName(ctx, req.BucketName)
// 	if err != nil {
// 		return nil, common.ParamErr.WithErr(err)
// 	}

// 	var expiresAt time.Time
// 	// 设置了失效时间
// 	if req.ExpiresIn > 0 {
// 		expiresAt = time.Now().Add(time.Duration(req.ExpiresIn) * time.Second)
// 	}

// 	url := CreatePresignedURL(ak, sk, bucket.Region, fmt.Sprintf("%s.%s", bucket.Name, consts.ServerName), bucket.Name, req.ObjectKey, "GET", req.ExpiresIn)

// 	return &dto.CreatePresignedUrlResp{
// 		URL:       url,
// 		ExpiresAt: expiresAt.UnixMilli(),
// 	}, common.OK
// }

// func deriveSigningKey(secretKey, date, region string) []byte {
// 	kDate := tools.HmacSHA256("OSS4"+secretKey, date)
// 	kRegion := tools.HmacSHA256(kDate, region)
// 	kService := tools.HmacSHA256(kRegion, consts.ServerName)
// 	kSigning := tools.HmacSHA256(kService, consts.Request)
// 	return []byte(kSigning)
// }

// func CreatePresignedURL(ak, sk, region, host, bucketName, objectKey, method string, expiresIn int64) string {
// 	now := time.Now().UTC()
// 	date := now.Format("20060102")
// 	datetime := now.Format("20060102T150405Z")

// 	// Credential
// 	credential := fmt.Sprintf("%s/%s/%s/%s/%s", ak, date, region, consts.ServerName, consts.Request)

// 	const (
// 		Algorithm = "OSS4-HMAC-SHA256"
// 	)

// 	// Step 1: 构造 Canonical Query String（不含 X-OSS-Signature）
// 	queryParams := url.Values{}
// 	queryParams.Set("X-OSS-Algorithm", Algorithm)
// 	queryParams.Set("X-OSS-Credential", credential)
// 	queryParams.Set("X-OSS-Date", datetime)
// 	if expiresIn != 0 {
// 		queryParams.Set("X-OSS-Expires", fmt.Sprintf("%d", expiresIn))
// 	}
// 	queryParams.Set("X-OSS-SignedHeaders", "host")

// 	// Query String 必须按字母排序
// 	keys := make([]string, 0, len(queryParams))
// 	for k := range queryParams {
// 		keys = append(keys, k)
// 	}
// 	sort.Strings(keys)
// 	canonicalQueryParts := make([]string, 0, len(keys))
// 	for _, k := range keys {
// 		canonicalQueryParts = append(canonicalQueryParts,
// 			url.QueryEscape(k)+"="+url.QueryEscape(queryParams.Get(k)))
// 	}
// 	canonicalQueryString := strings.Join(canonicalQueryParts, "&")

// 	// Step 1: 构造 Canonical Request
// 	canonicalRequest := strings.Join([]string{
// 		method,
// 		"/" + bucketName + "/" + objectKey,
// 		canonicalQueryString,
// 		"host:" + host + "\n", // canonical headers
// 		"host",                // signed headers
// 		"UNSIGNED-PAYLOAD",    // presigned URL 固定值
// 	}, "\n")

// 	// Step 2: StringToSign
// 	credentialScope := fmt.Sprintf("%s/%s/%s/%s", date, region, consts.ServerName, consts.Request)
// 	stringToSign := strings.Join([]string{
// 		Algorithm,
// 		datetime,
// 		credentialScope,
// 		tools.Sha256Hash(canonicalRequest),
// 	}, "\n")

// 	// Step 3: 派生 SigningKey
// 	signingKey := deriveSigningKey(sk, date, region)

// 	// Step 4: 计算签名
// 	signature := tools.HmacSHA256(string(signingKey), stringToSign)

// 	// 拼最终 URL
// 	return fmt.Sprintf("https://%s/%s/%s?%s&X-OSS-Signature=%s",
// 		host, bucketName, objectKey, canonicalQueryString, signature)
// }
