package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// generateID 生成随机 ID
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateLogID 生成日志 ID
func GenerateLogID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("log_%d_%s", time.Now().UnixNano(), hex.EncodeToString(b))
}

// GenerateKeyID 生成 API Key ID
func GenerateKeyID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "key_" + hex.EncodeToString(b)
}

// GenerateAPIKey 生成 API Key 值
func GenerateAPIKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "sk-fa-" + hex.EncodeToString(b)
}
