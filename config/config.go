package config

import (
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"oss/consts"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

var (
	EtcdKey         = fmt.Sprintf("config/%s/system", consts.ServerName)
	EtcdAddr        string
	localConfigPath string
	GlobalConfig    *Config
)

type Config struct {
	Server   Server             `yaml:"server"`
	Storage  Storage            `yaml:"storage"`
	Mysql    Mysql              `yaml:"mysql"`
	Redis    Redis              `yaml:"redis"`
	Security Security           `yaml:"security"`
	CORS     CORS               `yaml:"cors"`
	Video    Video              `yaml:"video"`
	AppConf  map[string]AppConf `yaml:"app_conf"`
}

type Storage struct {
	Type  string       `yaml:"type"`
	Local LocalStorage `yaml:"local"`
	S3    S3Storage    `yaml:"s3"`
	OSS   S3Storage    `yaml:"oss"`
	COS   S3Storage    `yaml:"cos"`
}

type LocalStorage struct {
	BaseDir string `yaml:"base_dir"`
}

type S3Storage struct {
	Endpoint        string `yaml:"endpoint"`
	Region          string `yaml:"region"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
	Bucket          string `yaml:"bucket"`
	DisableSSL      bool   `yaml:"disable_ssl"`
	ForcePathStyle  bool   `yaml:"force_path_style"`
}

type Server struct {
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	EnablePprof bool   `yaml:"enable_prof"`
	LogLevel    string `yaml:"log_level"`
	SaveDir     string `yaml:"save_dir"`
	Env         string `yaml:"env"`
}

func (s Storage) GetProviderConfig(storageType string) S3Storage {
	switch strings.ToLower(strings.TrimSpace(storageType)) {
	case "oss":
		return s.OSS
	case "cos":
		return s.COS
	default:
		return s.S3
	}
}

type Security struct {
	AESKey                string `yaml:"aes_key"` // base64 encoded AES key. Decoded length must be 16, 24, or 32 bytes.
	ReplayWindowSeconds   int    `yaml:"replay_window_seconds"`
	S3ReplayWindowSeconds int    `yaml:"s3_replay_window_seconds"`
}

func (s Security) AESKeyBytes() ([]byte, error) {
	raw := strings.TrimSpace(s.AESKey)
	if raw == "" {
		return nil, fmt.Errorf("security.aes_key is required")
	}

	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("security.aes_key must be valid base64: %w", err)
	}
	if !isAESKeyLength(len(decoded)) {
		return nil, fmt.Errorf("security.aes_key decoded length must be 16, 24, or 32 bytes, got %d", len(decoded))
	}
	return decoded, nil
}

func (s Security) GetReplayWindow() time.Duration {
	if s.ReplayWindowSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(s.ReplayWindowSeconds) * time.Second
}

func (s Security) GetS3ReplayWindow() time.Duration {
	if s.S3ReplayWindowSeconds <= 0 {
		return 15 * time.Minute
	}
	return time.Duration(s.S3ReplayWindowSeconds) * time.Second
}

type CORS struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
	AllowedMethods []string `yaml:"allowed_methods"`
	AllowedHeaders []string `yaml:"allowed_headers"`
	MaxAgeSeconds  int32    `yaml:"max_age_seconds"`
}

type Video struct {
	TranscodeMaxConcurrency int `yaml:"transcode_max_concurrency"`
	SegmentDurationSeconds  int `yaml:"segment_duration_seconds"`
	PlayTokenTTLSeconds     int `yaml:"play_token_ttl_seconds"`
}

func (v Video) GetTranscodeMaxConcurrency() int {
	if v.TranscodeMaxConcurrency <= 0 {
		return consts.DefaultTranscodeMaxConcurrency
	}
	return v.TranscodeMaxConcurrency
}

func (v Video) GetSegmentDurationSeconds() int {
	if v.SegmentDurationSeconds <= 0 {
		return consts.HLSSegmentDurationSeconds
	}
	return v.SegmentDurationSeconds
}

func (v Video) GetPlayTokenTTLSeconds() int {
	if v.PlayTokenTTLSeconds <= 0 {
		return consts.DefaultPlayTokenTTLSeconds
	}
	return v.PlayTokenTTLSeconds
}

type Mysql struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	DBName   string `yaml:"db_name"`
	Charset  string `yaml:"charset"`
	ShowSql  bool   `yaml:"show_sql"`
	MaxOpen  int    `yaml:"max_open"`
	MaxIdle  int    `yaml:"max_idle"`
}

func (m *Mysql) GetDsn() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=Local",
		m.User, m.Password, m.Host, m.Port, m.DBName, m.Charset)
}

type Redis struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	MaxIdle  int    `yaml:"max_idle"`
	MaxOpen  int    `yaml:"max_open"`
}

// TODO : 添加 AppConf 结构体定义，根据实际需求添加字段
type AppConf struct {
}

var envKeys = []string{
	"server.host",
	"server.port",
	"server.enable_prof",
	"server.log_level",
	"server.save_dir",
	"server.env",
	"mysql.user",
	"mysql.password",
	"mysql.host",
	"mysql.port",
	"mysql.db_name",
	"mysql.charset",
	"mysql.show_sql",
	"mysql.max_open",
	"mysql.max_idle",
	"redis.addr",
	"redis.password",
	"redis.db",
	"redis.max_idle",
	"redis.max_open",
	"security.aes_key",
	"cors.allowed_origins",
	"cors.allowed_methods",
	"cors.allowed_headers",
	"cors.max_age_seconds",
	"video.transcode_max_concurrency",
	"video.segment_duration_seconds",
	"video.play_token_ttl_seconds",
	"storage.type",
	"storage.local.base_dir",
	"storage.s3.endpoint",
	"storage.s3.region",
	"storage.s3.access_key_id",
	"storage.s3.secret_access_key",
	"storage.s3.bucket",
	"storage.s3.disable_ssl",
	"storage.s3.force_path_style",
	"storage.oss.endpoint",
	"storage.oss.region",
	"storage.oss.access_key_id",
	"storage.oss.secret_access_key",
	"storage.oss.bucket",
	"storage.oss.disable_ssl",
	"storage.oss.force_path_style",
	"storage.cos.endpoint",
	"storage.cos.region",
	"storage.cos.access_key_id",
	"storage.cos.secret_access_key",
	"storage.cos.bucket",
	"storage.cos.disable_ssl",
	"storage.cos.force_path_style",
	"security.replay_window_seconds",
	"security.s3_replay_window_seconds",
}

func init() {
	flag.StringVar(&localConfigPath, "c", "./config.yaml", "config file path")
	flag.StringVar(&EtcdAddr, "e", os.Getenv("ETCD_ADDR"), "etcd address")
}

func InitConfig() *Config {
	var (
		err     error
		tempCfg *Config
		vipConf = viper.New()
	)

	vipConf.SetConfigType("yaml")
	if err = configureEnv(vipConf); err != nil {
		panic(err)
	}

	flag.Parse()

	// 优先从 etcd 加载配置
	if EtcdAddr != "" {
		tempCfg, err = loadConfigFromEtcd(vipConf)
		if err != nil {
			panic(err)
		}

		GlobalConfig = tempCfg

		return tempCfg
	}

	tempCfg, err = loadConfigFromFile(vipConf)
	if err != nil {
		panic(err)
	}

	GlobalConfig = tempCfg
	return tempCfg
}

func configureEnv(viper *viper.Viper) error {
	viper.SetEnvPrefix("OSS")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	for _, key := range envKeys {
		if err := viper.BindEnv(key); err != nil {
			return err
		}
	}
	return nil
}

func unmarshalAndValidate(viper *viper.Viper) (*Config, error) {
	tempConf := &Config{}
	if err := viper.Unmarshal(tempConf, func(config *mapstructure.DecoderConfig) {
		config.TagName = "yaml"
	}); err != nil {
		return nil, err
	}
	if err := ValidateConfig(tempConf); err != nil {
		return nil, err
	}
	return tempConf, nil
}

func ValidateConfig(conf *Config) error {
	if conf == nil {
		return fmt.Errorf("config is nil")
	}
	aesKey, err := conf.Security.AESKeyBytes()
	if err != nil {
		return err
	}
	if strings.TrimSpace(conf.Mysql.Host) == "" {
		return fmt.Errorf("mysql.host is required")
	}
	if strings.TrimSpace(conf.Redis.Addr) == "" {
		return fmt.Errorf("redis.addr is required")
	}

	if strings.EqualFold(strings.TrimSpace(conf.Server.Env), "prod") {
		if isWeakSecret(conf.Mysql.Password) {
			return fmt.Errorf("mysql.password uses a weak default value in prod")
		}
		if isWeakSecret(conf.Redis.Password) {
			return fmt.Errorf("redis.password uses a weak default value in prod")
		}
		if isWeakAESKey(aesKey) {
			return fmt.Errorf("security.aes_key uses an example value in prod")
		}
		if hasWildcard(conf.CORS.AllowedOrigins) {
			return fmt.Errorf("cors.allowed_origins cannot contain * in prod")
		}
	}
	if conf.Security.ReplayWindowSeconds < 0 {
		return fmt.Errorf("security.replay_window_seconds cannot be negative")
	}
	if conf.Security.S3ReplayWindowSeconds < 0 {
		return fmt.Errorf("security.s3_replay_window_seconds cannot be negative")
	}

	storageType := strings.ToLower(strings.TrimSpace(conf.Storage.Type))
	switch storageType {
	case "", "local":
		// local storage is always valid with save_dir or storage.local.base_dir
	case "s3", "oss", "cos":
		providerCfg := conf.Storage.GetProviderConfig(storageType)
		if strings.TrimSpace(providerCfg.Region) == "" {
			return fmt.Errorf("storage.%s.region is required", storageType)
		}
		if strings.TrimSpace(providerCfg.AccessKeyID) == "" {
			return fmt.Errorf("storage.%s.access_key_id is required", storageType)
		}
		if strings.TrimSpace(providerCfg.SecretAccessKey) == "" {
			return fmt.Errorf("storage.%s.secret_access_key is required", storageType)
		}
	default:
		return fmt.Errorf("unsupported storage.type: %s", conf.Storage.Type)
	}

	return nil
}

func isAESKeyLength(length int) bool {
	return length == 16 || length == 24 || length == 32
}

func isWeakSecret(value string) bool {
	normalized := strings.TrimSpace(strings.ToLower(value))
	return normalized == "change_me" || strings.Contains(normalized, "change_me") || strings.Contains(normalized, "example")
}

func isWeakAESKey(key []byte) bool {
	return len(key) == 16 && string(key) == "0123456789abcdef" || len(key) == 32 && string(key) == "0123456789abcdef0123456789abcdef"
}

func hasWildcard(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "*" {
			return true
		}
	}
	return false
}

func loadConfigFromEtcd(viper *viper.Viper) (*Config, error) {
	if err := viper.AddRemoteProvider("etcd", EtcdAddr, EtcdKey); err != nil {
		return nil, err
	}
	if err := viper.ReadRemoteConfig(); err != nil {
		return nil, err
	}
	tempConf, err := unmarshalAndValidate(viper)
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			time.Sleep(time.Second * 30)
			if err := viper.WatchRemoteConfig(); err == nil {
				updated, err := unmarshalAndValidate(viper)
				if err != nil {
					fmt.Printf("failed to reload remote config: %v\n", err)
					continue
				}
				*tempConf = *updated
			}
		}

	}()
	return tempConf, nil
}

// Dev 开发
func loadConfigFromFile(viper *viper.Viper) (*Config, error) {
	viper.SetConfigFile(localConfigPath)
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	tempConf, err := unmarshalAndValidate(viper)
	if err != nil {
		return nil, err
	}

	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		updated, err := unmarshalAndValidate(viper)
		if err != nil {
			fmt.Printf("failed to reload config: %v\n", err)
			return
		}
		*tempConf = *updated
	})

	return tempConf, nil
}
