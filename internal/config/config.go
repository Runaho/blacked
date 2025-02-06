package config

import (
	"strconv"
	"time"
)

type ServerConfig struct {
	Scheme string `koanf:"scheme" default:"http"`
	Port   int    `koanf:"port" default:"8082"`
	Host   string `koanf:"host" default:"localhost"`

	ReadTimeout     time.Duration `koanf:"read_timeout" default:"5s"`
	WriteTimeout    time.Duration `koanf:"write_timeout" default:"10s"`
	ShutdownTimeout time.Duration `koanf:"shutdown_timeout" default:"30s"`

	AllowOrigins []string `koanf:"alloworigins" default:"[]"`
	HealthCheck  bool     `koanf:"health_check" default:"true"`
}

func (s *ServerConfig) GetServerURL() string {
	return s.Scheme + "://" + s.Host + ":" + strconv.Itoa(s.Port)
}

type CacheConfig struct {
	RefreshInterval time.Duration `koanf:"cache_refresh_interval" default:"5m"`
}

type APPConfig struct {
	Environtment string `koanf:"environtment" default:"development"`
}

type Config struct {
	APP    APPConfig
	Server ServerConfig
	Cache  CacheConfig
}
