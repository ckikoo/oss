package config

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/gogf/gf/util/gconv"
	"github.com/spf13/viper"
)

const (
	ServerName = "oss-server"
)

var (
	EtcdKey         = fmt.Sprintf("config/%s/system", ServerName)
	EtcdAddr        string
	localConfigPath string
	GlobalConfig    *Config
)

type Config struct {
	Server   Server             `yaml:"server"`
	Mysql    Mysql              `yaml:"mysql"`
	Redis    Redis              `yaml:"redis"`
	Security Security           `yaml:"security"`
	AppConf  map[string]AppConf `yaml:"app_conf"`
}

type Server struct {
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	EnablePprof bool   `yaml:"enable_prof"`
	LogLevel    string `yaml:"log_level"`
}

type Security struct {
	AESKey string `yaml:"aes_key"` // 32 字节 base64 编码的 AES-256 密钥
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
	fmt.Println(gconv.String(m))

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
		time.Sleep(time.Second)
		if err := viper.WatchRemoteConfig(); err == nil {
			_ = viper.Unmarshal(GlobalConfig)
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

	return tempConf, nil
}
