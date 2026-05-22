package tools

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
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
	padded := PKCS7Padding([]byte(plaintext), blockSize)

	// 随机生成 IV（每次加密都不同）
	iv := make([]byte, blockSize)
	if _, err = io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	// IV 前置 + 密文后置，拼成一个 slice
	out := make([]byte, blockSize+len(padded))
	copy(out[:blockSize], iv)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out[blockSize:], padded)

	return hex.EncodeToString(out), nil
}

// AESDecrypt 解密：从头部读取 IV，再解密剩余密文
func AESDecrypt(cryptotext string, key []byte) ([]byte, error) {
	data, err := hex.DecodeString(cryptotext)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	blockSize := block.BlockSize()
	if len(data) < blockSize*2 { // 至少要有 IV + 一个块的密文
		return nil, errors.New("ciphertext too short")
	}

	// 前 blockSize 字节是 IV，其余是密文
	iv := data[:blockSize]
	ciphertext := data[blockSize:]

	if len(ciphertext)%blockSize != 0 {
		return nil, errors.New("ciphertext is not a multiple of block size")
	}

	cipher.NewCBCDecrypter(block, iv).CryptBlocks(ciphertext, ciphertext)
	return PKCS7UnPadding(ciphertext), nil
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

	expectedBytes, err := hex.DecodeString(expected)
	if err != nil {
		return false
	}

	return hmac.Equal(expectedBytes, sig)
}
