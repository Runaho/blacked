package config

import (
	"strconv"
	"time"

	"github.com/rs/zerolog"
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
	UseBloom  bool           `koanf:"use_bloom" default:"true"`
	CacheType string         `koanf:"cache_type" default:"badger"` // Options: "badger"
	TTL       *time.Duration `kaonf:"ttl" default:"5m"`
}

type APPConfig struct {
	Environtment string        `koanf:"environtment" default:"development"`
	LogLevel     zerolog.Level `koanf:"log_level" default:"debug"`
}

type CollectorConfig struct {
	Concurrency    int    `koanf:"concurrency" default:"10"`
	BatchSize      int    `koanf:"batch_size" default:"100"`
	CronSchedule   string `koanf:"cron_schedule" default:"0 0 0 * * *"`
	StoreResponses bool   `koanf:"store_responses" default:"true"`
	StorePath      string `koanf:"store_path" default:"./responses"`
}

type ProviderConfig struct {
	EnabledProviders []string          `koanf:"enabled_providers"` // List of enabled providers if is empty all providers are enabled
	CronSchedules    map[string]string `koanf:"provider_crons"`    // Provider-specific cron schedules
	RunAtStartup     bool              `koanf:"run_at_startup" default:"true"`
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
	Provider  ProviderConfig
}
