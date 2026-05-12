package tools

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
)

func GenerateRandomKey(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("invalid key length")
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(buf)), nil
}

func UUIDHex() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}
