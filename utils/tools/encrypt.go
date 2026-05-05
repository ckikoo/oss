package tools

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func Sha256Hash(text string) string {
	// 创建一个新的sha256哈希对象
	hash := sha256.New()

	// 将字符串写入哈希对象
	hash.Write([]byte(text))

	// 从哈希对象中获取哈希值
	hashBytes := hash.Sum(nil)

	// 将字节切片转换为十六进制字符串
	return hex.EncodeToString(hashBytes)
}

func Md5Hash(text string) string {
	hash := md5.New()
	hash.Write([]byte(text))
	hashBytes := hash.Sum(nil)
	return hex.EncodeToString(hashBytes)
}

// AESEncrypt 加密函数
func AESEncrypt(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	blockSize := block.BlockSize()
	origData := PKCS7Padding([]byte(plaintext), blockSize)
	iv := make([]byte, blockSize)
	blockMode := cipher.NewCBCEncrypter(block, iv)

	encrypted := make([]byte, len(origData))
	blockMode.CryptBlocks(encrypted, origData)

	return fmt.Sprintf("%x", encrypted), nil
}

// AESDecrypt 解密函数
func AESDecrypt(cryptotext string, key []byte) ([]byte, error) {
	cryptoBytes, err := hex.DecodeString(cryptotext)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	blockSize := block.BlockSize()
	iv := make([]byte, blockSize)
	blockMode := cipher.NewCBCDecrypter(block, iv)

	origData := make([]byte, len(cryptoBytes))
	blockMode.CryptBlocks(origData, cryptoBytes)

	return PKCS7UnPadding(origData), nil
}

// PKCS7Padding 填充函数
func PKCS7Padding(src []byte, blockSize int) []byte {
	padding := blockSize - len(src)%blockSize
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(src, padText...)
}

// PKCS7UnPadding 去除填充函数
func PKCS7UnPadding(src []byte) []byte {
	length := len(src)
	unpadding := int(src[length-1])
	if unpadding < 1 || unpadding > aes.BlockSize {
		unpadding = 0
	}
	return src[:(length - unpadding)]
}

// 签名
func HmacSHA256(stringToSign, secretKey string) string {
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(stringToSign))
	return hex.EncodeToString(mac.Sum(nil))
}

// 验签（用 hmac.Equal 防时序攻击）
func HmacSHA256Verify(stringToSign, secretKey, signature string) bool {
	expected := HmacSHA256(stringToSign, secretKey)
	sig, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(expected), sig)
}
