package config

import (
	"flag"
	"fmt"
	"os"
	"oss/consts"
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
	Mysql    Mysql              `yaml:"mysql"`
	Redis    Redis              `yaml:"redis"`
	Security Security           `yaml:"security"`
	CORS     CORS               `yaml:"cors"`
	Video    Video              `yaml:"video"`
	AppConf  map[string]AppConf `yaml:"app_conf"`
}

type Server struct {
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	EnablePprof bool   `yaml:"enable_prof"`
	LogLevel    string `yaml:"log_level"`
	SaveDir     string `yaml:"save_dir"`
	Env         string `yaml:"env"`
}

type Security struct {
	AESKey string `yaml:"aes_key"` // 32 字节 base64 编码的 AES-256 密钥
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

func init() {
	flag.StringVar(&localConfigPath, "c", "./config.yaml", "config file path")
	flag.StringVar(&EtcdAddr, "e", os.Getenv("ETCD_ADDR"), "etcd address")
}

func InitConfig() *Config {
	var (
		err     error
		tempCfg *Config = &Config{}
		vipConf         = viper.New()
	)

	vipConf.SetConfigType("yaml")
	err = vipConf.Unmarshal(&tempCfg, func(config *mapstructure.DecoderConfig) {
		config.TagName = "yaml"
	})

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

func loadConfigFromEtcd(viper *viper.Viper) (*Config, error) {
	tempConf := &Config{}

	if err := viper.AddRemoteProvider("etcd", EtcdAddr, EtcdKey); err != nil {
		return nil, err
	}
	if err := viper.ReadRemoteConfig(); err != nil {
		return nil, err
	}
	if err := viper.Unmarshal(tempConf, func(config *mapstructure.DecoderConfig) {
		config.TagName = "yaml"
	}); err != nil {
		return nil, err
	}

	go func() {
		for {
			time.Sleep(time.Second * 30)
			if err := viper.WatchRemoteConfig(); err == nil {
				viper.Unmarshal(tempConf, func(config *mapstructure.DecoderConfig) {
					config.TagName = "yaml"
				})
			}
		}

	}()
	return tempConf, nil
}

// Dev 开发
func loadConfigFromFile(viper *viper.Viper) (*Config, error) {
	tempConf := &Config{}

	viper.SetConfigFile(localConfigPath)
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	if err := viper.Unmarshal(tempConf, func(config *mapstructure.DecoderConfig) {
		config.TagName = "yaml"
	}); err != nil {
		return nil, err
	}

	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		if err := viper.Unmarshal(tempConf, func(config *mapstructure.DecoderConfig) {
			config.TagName = "yaml"
		}); err != nil {
			fmt.Printf("failed to reload config: %v\n", err)
		}
	})

	return tempConf, nil
}
