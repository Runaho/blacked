package config

import (
	"errors"
	"os"
	"sync"

	"github.com/knadh/koanf/v2"

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

		if _err := defaults.Set(_config); _err != nil {
			err = _err
			return
		}

		if err := _k.Load(file.Provider("./"+configFile), toml.Parser()); err != nil {
			log.Error().Msg("error loading config [TOML]")
		} else {
			log.Info().Msg("config loaded from file")
		}

		_k.Load(file.Provider("./.env"), dotenv.Parser())

		if err := _k.Unmarshal("", _config); err != nil {
			log.Error().Msg("error unmarshalling config")
			panic(err)
		}

		if _config == emptyConfig {
			err = errors.New("config is empty")
			return
		}

		// Default any nil Enabled to true (backward-compat behavior: empty = all enabled)
		if _config.Providers != nil {
			for _, opts := range _config.Providers {
				if opts != nil && opts.Enabled == nil {
					enabled := true
					opts.Enabled = &enabled
				}
			}
		}

		zerolog.SetGlobalLevel(_config.APP.LogLevel)
	})

	return err
}

func IsDevMode() bool {
	if _config == nil {
		return true
	}

	return (_config.APP.Environment == "development")
}

// LoadScoringConfig reads config/scoring.toml and returns provider/source trust scores.
// Returns nil if the file can't be loaded — callers should fall back to defaults.
// SourceTrust scores override ProviderTrust scores when available.
func LoadScoringConfig() map[string]float64 {
	k := koanf.New(".")
	if err := k.Load(file.Provider("config/scoring.toml"), toml.Parser()); err != nil {
		log.Warn().Err(err).Msg("scoring.toml not loaded, using default trust scores")
		return nil
	}

	out := make(map[string]float64)

	// Load ProviderTrust (fallback scores)
	if raw := k.Get("ProviderTrust"); raw != nil {
		if m, ok := raw.(map[string]any); ok {
			for key, val := range m {
				switch v := val.(type) {
				case float64:
					out[key] = v
				case int64:
					out[key] = float64(v)
				case int:
					out[key] = float64(v)
				}
			}
		}
	}

	// Load SourceTrust (overrides provider defaults)
	if raw := k.Get("SourceTrust"); raw != nil {
		if m, ok := raw.(map[string]any); ok {
			for key, val := range m {
				switch v := val.(type) {
				case float64:
					out[key] = v
				case int64:
					out[key] = float64(v)
				case int:
					out[key] = float64(v)
				}
			}
		}
	}

	return out
}

func (c *Config) ProviderEnabled(name string) bool {
	if c.Providers == nil {
		return true
	}
	opts, ok := c.Providers[name]
	if !ok || opts == nil {
		return true
	}
	if opts.Enabled == nil {
		return true
	}
	return *opts.Enabled
}
