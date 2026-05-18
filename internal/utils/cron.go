package utils

import (
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
)

// ParseTTLFromCron extracts a sensible TTL from a cron schedule or duration string.
// Uses robfig/cron parser for accurate interval calculation from cron expressions.
func ParseTTLFromCron(cronSchedule string) time.Duration {
	if d, err := time.ParseDuration(cronSchedule); err == nil {
		return d
	}

	if cronSchedule == "" {
		log.Debug().Msg("no cron schedule configured, using default 6h TTL")
		return 6 * time.Hour
	}

	// Try robfig/cron parser for accurate interval from cron expressions
	sched, err := cron.ParseStandard(cronSchedule)
	if err == nil {
		now := time.Now()
		first := sched.Next(now)
		second := sched.Next(first)
		diff := second.Sub(first)
		if diff > 0 && diff <= 7*24*time.Hour {
			log.Debug().
				Str("cron", cronSchedule).
				Dur("ttl", diff).
				Msg("parsed TTL from cron schedule")
			return diff
		}
	}

	scheduleLower := strings.ToLower(cronSchedule)
	if strings.Contains(scheduleLower, "hour") || strings.Contains(scheduleLower, "h") {
		return 1 * time.Hour
	}
	if strings.Contains(scheduleLower, "day") || strings.Contains(scheduleLower, "d") {
		return 24 * time.Hour
	}
	if strings.Contains(scheduleLower, "week") || strings.Contains(scheduleLower, "w") {
		return 7 * 24 * time.Hour
	}

	log.Debug().Str("cron", cronSchedule).Msg("using default 6h TTL for unknown cron format")
	return 6 * time.Hour
}
