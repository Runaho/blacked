package config

import (
	"errors"
	"os"
	"sync"

	"github.com/knadh/koanf/v2"

	"slices"

	"github.com/creasty/defaults"
	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/file"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	_k      *koanf.Koanf
	_config *Config
	once    sync.Once
)

func GetConfig() *Config {
	if _config == nil {
		log.Info().Msg("config is nil trying to init")
		if err := InitConfig(); err != nil {
			log.Error().Msgf("error initializing config: %v", err)
		}
	}

	return _config
}

func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func InitConfig() error {
	var err error
	once.Do(func() {
		_k = koanf.New(".")

		_config = &Config{}
		emptyConfig := &Config{}

		configFile := GetEnv("CONFIG_FILE", ".env.toml")

		if err := _k.Load(file.Provider(""+configFile), toml.Parser()); err != nil {
			log.Error().Msgf("error loading config [TOML]: %v", err)
		}

		_k.Load(file.Provider(".env"), dotenv.Parser())

		if _err := defaults.Set(_config); _err != nil {
			err = _err
			return
		}

		_k.Unmarshal("", _config)

		log.Trace().Msgf("k: %+v", _config)

		if _config == emptyConfig {
			err = errors.New("config is empty")
			return
		}

		zerolog.SetGlobalLevel(_config.APP.LogLevel)
	})

	return err
}

func IsDevMode() bool {
	if _config == nil {
		return true
	}

	return (_config.APP.Environtment == "development")
}

// Returns the cron schedule for the provider.
// If the provider is not enabled, it returns an empty string.
func (c *Config) GetProviderCronSchedule(providerName string) (cronSchedule string, isExist bool) {
	if c.Provider.CronSchedules == nil {
		return "", false
	}

	if schedule, exists := c.Provider.CronSchedules[providerName]; exists {
		return schedule, true
	}

	return "", false
}

// IsProviderEnabled checks if the provider is enabled in the configuration.
// If the provider is not enabled, it returns false.
// If the list is not defined, it returns true.
func (c *ProviderConfig) IsProviderEnabled(providerName string) bool {
	if c.EnabledProviders == nil {
		return true
	}

	return slices.Contains(c.EnabledProviders, providerName)
}
