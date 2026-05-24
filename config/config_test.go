package config

import (
	"encoding/base64"
	"oss/consts"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestVideoDefaults(t *testing.T) {
	v := Video{}

	if got := v.GetTranscodeMaxConcurrency(); got != consts.DefaultTranscodeMaxConcurrency {
		t.Fatalf("GetTranscodeMaxConcurrency() = %d, want %d", got, consts.DefaultTranscodeMaxConcurrency)
	}
	if got := v.GetSegmentDurationSeconds(); got != consts.HLSSegmentDurationSeconds {
		t.Fatalf("GetSegmentDurationSeconds() = %d, want %d", got, consts.HLSSegmentDurationSeconds)
	}
	if got := v.GetPlayTokenTTLSeconds(); got != consts.DefaultPlayTokenTTLSeconds {
		t.Fatalf("GetPlayTokenTTLSeconds() = %d, want %d", got, consts.DefaultPlayTokenTTLSeconds)
	}
}

func TestVideoConfiguredValues(t *testing.T) {
	v := Video{
		TranscodeMaxConcurrency: 3,
		SegmentDurationSeconds:  6,
		PlayTokenTTLSeconds:     60,
	}

	if got := v.GetTranscodeMaxConcurrency(); got != 3 {
		t.Fatalf("GetTranscodeMaxConcurrency() = %d, want 3", got)
	}
	if got := v.GetSegmentDurationSeconds(); got != 6 {
		t.Fatalf("GetSegmentDurationSeconds() = %d, want 6", got)
	}
	if got := v.GetPlayTokenTTLSeconds(); got != 60 {
		t.Fatalf("GetPlayTokenTTLSeconds() = %d, want 60", got)
	}
}

func TestSecurityAESKeyBytes(t *testing.T) {
	key := "0123456789abcdef"
	encoded := base64.StdEncoding.EncodeToString([]byte(key))

	got, err := (Security{AESKey: encoded}).AESKeyBytes()
	if err != nil {
		t.Fatalf("AESKeyBytes() error = %v", err)
	}
	if string(got) != key {
		t.Fatalf("AESKeyBytes() = %q, want %q", string(got), key)
	}
}

func TestValidateConfigRejectsInvalidAESKey(t *testing.T) {
	cfg := validTestConfig()
	cfg.Security.AESKey = "not-base64"

	if err := ValidateConfig(cfg); err == nil {
		t.Fatalf("ValidateConfig() error = nil, want invalid AES key error")
	}
}

func TestValidateConfigRejectsMissingBackends(t *testing.T) {
	cfg := validTestConfig()
	cfg.Mysql.Host = ""
	if err := ValidateConfig(cfg); err == nil {
		t.Fatalf("ValidateConfig() error = nil, want missing mysql host error")
	}

	cfg = validTestConfig()
	cfg.Redis.Addr = ""
	if err := ValidateConfig(cfg); err == nil {
		t.Fatalf("ValidateConfig() error = nil, want missing redis addr error")
	}
}

func TestValidateConfigRejectsProdWeakDefaults(t *testing.T) {
	cfg := validTestConfig()
	cfg.Server.Env = "prod"
	cfg.Mysql.Password = "CHANGE_ME"
	if err := ValidateConfig(cfg); err == nil {
		t.Fatalf("ValidateConfig() error = nil, want weak mysql password error")
	}

	cfg = validTestConfig()
	cfg.Server.Env = "prod"
	cfg.CORS.AllowedOrigins = []string{"*"}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatalf("ValidateConfig() error = nil, want wildcard cors error")
	}
}

func TestEnvOverridesConfig(t *testing.T) {
	t.Setenv("OSS_SERVER_PORT", "19090")
	t.Setenv("OSS_MYSQL_HOST", "mysql-from-env")
	t.Setenv("OSS_REDIS_ADDR", "redis-from-env:6379")
	t.Setenv("OSS_SECURITY_AES_KEY", base64.StdEncoding.EncodeToString([]byte("env-key-16-bytes")))

	vip := viper.New()
	vip.SetConfigType("yaml")
	if err := configureEnv(vip); err != nil {
		t.Fatalf("configureEnv() error = %v", err)
	}
	if err := vip.ReadConfig(strings.NewReader(`
server:
  port: 8080
  env: dev
mysql:
  host: mysql-from-file
  password: file-secret
redis:
  addr: redis-from-file:6379
  password: file-secret
security:
  aes_key: MDEyMzQ1Njc4OWFiY2RlZg==
cors:
  allowed_origins:
    - "*"
`)); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	cfg, err := unmarshalAndValidate(vip)
	if err != nil {
		t.Fatalf("unmarshalAndValidate() error = %v", err)
	}
	if cfg.Server.Port != 19090 {
		t.Fatalf("Server.Port = %d, want 19090", cfg.Server.Port)
	}
	if cfg.Mysql.Host != "mysql-from-env" {
		t.Fatalf("Mysql.Host = %q, want mysql-from-env", cfg.Mysql.Host)
	}
	if cfg.Redis.Addr != "redis-from-env:6379" {
		t.Fatalf("Redis.Addr = %q, want redis-from-env:6379", cfg.Redis.Addr)
	}
}

func validTestConfig() *Config {
	return &Config{
		Server: Server{Env: "dev"},
		Mysql: Mysql{
			Host:     "127.0.0.1",
			Password: "mysql-secret",
		},
		Redis: Redis{
			Addr:     "127.0.0.1:6379",
			Password: "redis-secret",
		},
		Security: Security{
			AESKey: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef")),
		},
		CORS: CORS{AllowedOrigins: []string{"*"}},
	}
}
