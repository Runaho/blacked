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

type CacheSettings struct {
	RefreshInterval time.Duration `koanf:"cache_refresh_interval" default:"5m"`
}

type APPConfig struct {
	Environtment string `koanf:"environtment" default:"development"`
}

type CollectorConfig struct {
	MaxWorkers     int    `koanf:"max_workers" default:"10"`
	BatchSize      int    `koanf:"batch_size" default:"100"`
	Cron           string `koanf:"cron" default:"0 0 0 * * *"`
	StoreResponses bool   `koanf:"store_responses" default:"true"`
	StorePath      string `koanf:"store_path" default:"./responses"`
}

type CollyConfig struct {
	MaxRedirects int           `koanf:"max_redirects" default:"10"`
	MaxSize      int           `koanf:"max_size" default:"1048576"`
	MaxDepth     int           `koanf:"max_depth" default:"1"`
	UserAgent    string        `koanf:"user_agent" default:"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.3"`
	TimeOut      time.Duration `koanf:"timeout" default:"5m"`
}

type Config struct {
	APP       APPConfig
	Server    ServerConfig
	Cache     CacheSettings
	Collector CollectorConfig
	Colly     CollyConfig
}
