package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"oss/consts"
	"oss/service/dto"
	"oss/utils/tools"
	"strings"
	"time"

	"github.com/gogf/gf/util/gconv"
)

// ============================================================
// 响应结构体
// ============================================================

// ApiResponse 通用API响应包装
type ApiResponse struct {
	Code   int             `json:"code"`
	Msg    string          `json:"msg"`
	ErrMsg string          `json:"err_msg"`
	IsAes  bool            `json:"is_aes"`
	Data   json.RawMessage `json:"data"`
}

// ============================================================
// 配置区：修改这里
// ============================================================

const (
	accessKey = "40C34413888EBD0D8C2358B3CC7605C2"
	secretKey = "B9C72FAE566F6FD06D20E2C3561A0493"
	baseURL   = "http://localhost:8080"
)

// ============================================================
// 辅助函数
// ============================================================

// parseResponse 从 ApiResponse 中解析数据
func parseResponse(respBody string, target interface{}) error {
	var apiResp ApiResponse
	if err := json.Unmarshal([]byte(respBody), &apiResp); err != nil {
		return fmt.Errorf("解析响应失败: %v", err)
	}

	if apiResp.Code != 200 {
		return fmt.Errorf("API错误: %s (code: %d)", apiResp.Msg, apiResp.Code)
	}

	if err := json.Unmarshal(apiResp.Data, target); err != nil {
		return fmt.Errorf("解析数据字段失败: %v", err)
	}

	return nil
}

// ============================================================
// 签名工具
// ============================================================

// computeMD5 计算二进制数据的MD5值
func computeMD5(data []byte) string {
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}

func hmacSHA256(data, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func buildStringToSign(method, path, query, host, contentType, body string) string {
	var sb strings.Builder

	sb.WriteString(method + " " + path)
	if query != "" {
		sb.WriteString("?" + query)
	}
	sb.WriteString("\nHost: " + host)
	if contentType != "" {
		sb.WriteString("\nContent-Type: " + contentType)
	}
	sb.WriteString("\n\n")

	// 跳过二进制流和 multipart 请求的 body
	if contentType != "application/octet-stream" && !strings.Contains(contentType, "multipart/") && body != "" {
		sb.WriteString(body)
	}

	return sb.String()
}

func buildAuth(method, path, query, host, contentType, body string) string {
	timestamp := time.Now().Unix()

	stringToSign := buildStringToSign(method, path, query, host, contentType, body)

	signature := tools.HmacSHA256(stringToSign, secretKey)

	return fmt.Sprintf("OSS %s:%d:%s", accessKey, timestamp, signature)
}

// ============================================================
// 请求封装
// ============================================================

func sendRequest(method, urlPath, query, body, contentType string) (string, error) {
	return sendRequestWithHeaders(method, urlPath, query, body, contentType, nil)
}

// sendRequestWithHeaders 支持自定义header的请求函数
func sendRequestWithHeaders(method, urlPath, query, body, contentType string, headers map[string]string) (string, error) {
	fullURL := baseURL + urlPath
	if query != "" {
		fullURL += "?" + query
	}

	host := strings.TrimPrefix(baseURL, "http://")
	host = strings.TrimPrefix(host, "https://")

	auth := buildAuth(method, urlPath, query, host, contentType, body)

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, fullURL, reqBody)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", auth)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	// 添加自定义header
	if headers != nil {
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return string(respBody), nil
}

func doRequest(method, urlPath, query, body, contentType string) {
	fullURL := baseURL + urlPath
	if query != "" {
		fullURL += "?" + query
	}

	host := strings.TrimPrefix(baseURL, "http://")
	host = strings.TrimPrefix(host, "https://")

	auth := buildAuth(method, urlPath, query, host, contentType, body)

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, fullURL, reqBody)
	if err != nil {
		fmt.Println("创建请求失败:", err)
		return
	}

	req.Header.Set("Authorization", auth)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	fmt.Println("=== 请求信息 ===")
	fmt.Printf("Method: %s\n", method)
	fmt.Printf("URL: %s\n", fullURL)
	fmt.Printf("Authorization: %s\n", auth)
	fmt.Printf("body: %v\n", body)
	fmt.Println("================")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("请求失败:", err)
		return
	}
	defer resp.Body.Close()
	fmt.Printf("resp.Header: %v\n", resp.Header)
	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("=== 响应 [%d] ===\n", resp.StatusCode)
	fmt.Println(string(respBody))
	fmt.Println("================")
}

// 上传文件（multipart form-data）
func doUploadFile(method, urlPath string, fileContent, fileName string) {
	fullURL := baseURL + urlPath
	host := strings.TrimPrefix(baseURL, "http://")
	host = strings.TrimPrefix(host, "https://")

	// 构造 multipart form-data
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// 添加 file 字段
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		fmt.Println("创建 form file 失败:", err)
		return
	}
	io.WriteString(part, fileContent)

	writer.Close()

	contentType := writer.FormDataContentType()
	fmt.Printf("contentType: %v\n", contentType)
	auth := buildAuth(method, urlPath, "", host, contentType, "")

	req, err := http.NewRequest(method, fullURL, &buf)
	if err != nil {
		fmt.Println("创建请求失败:", err)
		return
	}

	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", contentType)

	fmt.Println("=== 请求信息 ===")
	fmt.Printf("Method: %s\n", method)
	fmt.Printf("URL: %s\n", fullURL)
	fmt.Printf("Authorization: %s\n", auth)
	fmt.Println("================")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("请求失败:", err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("=== 响应 [%d] ===\n", resp.StatusCode)
	fmt.Println(string(respBody))
	fmt.Println("================")
}

// ============================================================
// 测试用例：按需取消注释
// ============================================================

func createUploadToken() {
	req := dto.CreateUploadTokenReq{
		BucketName: "test-bucket",
		ObjectKey:  "test.txt",
		ExpiresIn:  3600,
	}

	doRequest(http.MethodPost, "/api/v1/upload/tokens", "", gconv.String(req), "application/json")
}

func createBucket() {
	req := dto.CreateBucketReq{
		UserID: 2,
		Name:   "test-bucket1",
		Region: "cn-east",
	}

	doRequest(http.MethodPost, "/api/v1/buckets", "", gconv.String(req), "application/json")
}

func ListBucket() {

	doRequest(http.MethodGet, "/api/v1/buckets", "", "", "")
}
func UpdateBucket() {
	// "test-bucket1"
	status := int32(consts.BucketStatusNormal)
	req := dto.UpdateBucketReq{
		Status: &status,
	}
	doRequest(http.MethodPatch, "/api/v1/buckets/test-bucket1", "", gconv.String(req), "application/json")
}

func DeleteBucket() {
	// "test-bucket1"
	// status := int32(consts.BucketStatusNormal)

	doRequest(http.MethodDelete, "/api/v1/buckets/test-bucket1", "", "", "")
}

func doCompleteMultipartUpload(fileContent, fileName, bucketName, objectKey string) {
	// 分片大小 100kb
	const chunkSize = 100 * 1024
	data := []byte(fileContent)
	totalSize := len(data)

	// 初始化分片上传
	createReq := dto.CreateMultipartUploadReq{
		ObjectKey:   objectKey,
		ContentType: "application/octet-stream",
		TotalChunk:  int32(math.Ceil(float64(totalSize) / chunkSize)), // 会动态计算
		Overwrite:   true,
	}

	body := gconv.String(createReq)
	resp, err := sendRequest("POST", "/api/v1/buckets/"+bucketName+"/multipart/uploads", "", body, "application/json")
	if err != nil {
		fmt.Println("初始化分片上传失败:", err)
		return
	}

	fmt.Printf("初始化响应: %v\n", resp)

	var createResp dto.CreateMultipartUploadResp
	if err := parseResponse(resp, &createResp); err != nil {
		fmt.Println("解析初始化响应失败:", err)
		return
	}
	uploadID := createResp.UploadID
	fmt.Printf("初始化成功，UploadID: %s\n", uploadID)

	defer func() {
		if r := recover(); r != nil {
			urlPath := fmt.Sprintf("/api/v1/buckets/%s/multipart/uploads/%s", bucketName, uploadID)
			resp, err := sendRequest("DELETE", urlPath, "", "", "")
			fmt.Printf("err: %v\n", err)
			fmt.Printf("resp: %v\n", resp)
		}
	}()

	parts := []dto.MultipartCompletePart{}
	partNumber := 1

	for offset := 0; offset < totalSize; offset += chunkSize {

		end := offset + chunkSize
		if end > totalSize {
			end = totalSize
		}
		chunk := data[offset:end]

		// 计算分片的MD5
		md5Hash := computeMD5(chunk)

		// 上传分片
		urlPath := fmt.Sprintf("/api/v1/buckets/%s/multipart/uploads/%s/parts/%d", bucketName, uploadID, partNumber)

		fmt.Printf("上传分片 %d: %s (MD5: %s)\n", partNumber, urlPath, md5Hash)

		// 添加 Content-MD5 header
		headers := map[string]string{
			"Content-MD5": md5Hash,
		}

		resp, err := sendRequestWithHeaders("PUT", urlPath, "", string(chunk), "application/octet-stream", headers)
		if err != nil {
			fmt.Printf("上传分片 %d 失败: %v\n", partNumber, err)
			return
		}

		var partResp dto.UploadMultipartPartResp
		if err := parseResponse(resp, &partResp); err != nil {
			fmt.Printf("解析分片响应失败: %v\n", err)
			return
		}

		parts = append(parts, dto.MultipartCompletePart{
			PartNumber: int32(partNumber),
			Etag:       partResp.Etag,
		})
		fmt.Printf("分片 %d 上传成功，Etag: %s\n", partNumber, partResp.Etag)
		partNumber++
	}

	// 完成上传
	completeReq := dto.CompleteMultipartUploadReq{
		Parts: parts,
	}
	body = gconv.String(completeReq)
	resp, err = sendRequest("POST", "/api/v1/buckets/"+bucketName+"/multipart/uploads/"+uploadID+"/complete", "", body, "application/json")
	if err != nil {
		fmt.Println("完成分片上传失败:", err)
		return
	}

	var completeResp dto.CompleteMultipartUploadResp
	if err := parseResponse(resp, &completeResp); err != nil {
		fmt.Println("解析完成响应失败:", err)
		return
	}

	fmt.Printf("分片上传完成，ObjectID: %d, ObjectKey: %s\n", completeResp.ObjectID, completeResp.ObjectKey)
}

func doMutipartUpload() {
	createReq := dto.CreateMultipartUploadReq{
		ObjectKey:   "text.txt",
		ContentType: "txt",
		TotalChunk:  4,
		Overwrite:   true,
		CallbackUrl: "https://baidu.com",
	}

	doRequest("POST", "/api/v1/buckets/test-bucket/multipart/uploads", "", gconv.String(createReq), "application/json")
}
func main() {
	// createUploadToken()
	// createBucket()
	// ListBucket()
	// UpdateBucket()
	// DeleteBucket()
	// ListBucket()

	// ---- 简单上传 ----
	// doUploadFile("PUT", "/api/v1/buckets/test-bucket/objects/test.txt", "hello world", "test.txt")

	// ---- 获取对象 ----
	// doRequest("GET", "/api/v1/buckets/test-bucket/objects/test.txt", "", "", "")

	// ---- 列出对象 ----
	// doRequest("GET", "/api/v1/buckets/test-bucket/objects", "", "", "")

	// ---- 删除对象 ----
	// doRequest("DELETE", "/api/v1/buckets/test-bucket/objects/test.txt", "", "", "")
	// doRequest("GET", "/api/v1/buckets/test-bucket/objects", "", "", "")
	// ---- 生成上传 Token ----
	// doRequest("POST", "/api/v1/upload/tokens", "", `{"bucket_name":"test-bucket","object_key":"test.txt","expires_in":3600}`, "application/json")

	// ---- 生成下载 Token ----
	// doRequest("POST", "/api/v1/download/tokens", "", `{"bucket_name":"test-bucket","object_key":"test.txt","expires_in":3600}`, "application/json")

	// ---- 初始化分片上传 ----
	// doMutipartUpload()

	// ---- 完整分片上传 ----
	largeContent := strings.Repeat("This is a large file content for testing multipart upload. ", 10000) // 大约 70KB
	doCompleteMultipartUpload(largeContent, "large.txt", "test-bucket", "large.txt")

	// ---- 默认跑一个测试 ----
	fmt.Println("请取消注释 main() 中需要测试的接口")

	// 快速生成一个 Authorization Header（不发请求，只打印）
	printAuthHeader("GET", "/api/v1/buckets", "", "", "")
}

// 只打印 Authorization，不发请求，方便 curl / postman 使用
func printAuthHeader(method, path, query, host, contentType string) {
	if host == "" {
		host = strings.TrimPrefix(baseURL, "http://")
		host = strings.TrimPrefix(host, "https://")
	}
	auth := buildAuth(method, path, query, host, contentType, "")
	fmt.Println("\n=== 复制到 Postman / curl ===")
	fmt.Printf("Authorization: %s\n", auth)
	fmt.Printf("\ncurl -X %s %s%s \\\n  -H 'Authorization: %s'\n", method, baseURL, path, auth)
}
