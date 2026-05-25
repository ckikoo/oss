package dto

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type ListObjectsCursor struct {
	IsDir     int32  `json:"is_dir"`
	ObjectKey string `json:"object_key"`
	ID        int64  `json:"id"`
}

func EncodeListObjectsCursor(cursor ListObjectsCursor) string {
	data, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func DecodeListObjectsCursor(cursor string) (ListObjectsCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return ListObjectsCursor{}, err
	}
	var out ListObjectsCursor
	if err := json.Unmarshal(data, &out); err != nil {
		return ListObjectsCursor{}, err
	}
	if out.ObjectKey == "" || out.ID <= 0 {
		return ListObjectsCursor{}, fmt.Errorf("invalid cursor")
	}
	return out, nil
}
