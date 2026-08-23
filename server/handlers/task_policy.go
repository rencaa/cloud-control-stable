package handlers

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"
)

const (
	defaultTaskTimeoutSeconds = 3600
	minTaskTimeoutSeconds     = 30
	maxTaskTimeoutSeconds     = 86400
	defaultCronTimezone       = "Asia/Shanghai"
)

func normalizeTaskTimeout(value int) int {
	if value == 0 {
		return defaultTaskTimeoutSeconds
	}
	return value
}

func validateTaskTimeout(value int) error {
	value = normalizeTaskTimeout(value)
	if value < minTaskTimeoutSeconds || value > maxTaskTimeoutSeconds {
		return fmt.Errorf("timeout_seconds must be between %d and %d", minTaskTimeoutSeconds, maxTaskTimeoutSeconds)
	}
	return nil
}

func normalizeCronTimezone(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultCronTimezone
	}
	if len(value) > 64 {
		return "", fmt.Errorf("cron timezone is too long")
	}
	if _, err := time.LoadLocation(value); err != nil {
		return "", fmt.Errorf("invalid cron timezone %q", value)
	}
	return value, nil
}

func normalizeMisfirePolicy(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = "latest"
	}
	switch value {
	case "skip", "run_once", "latest":
		return value, nil
	default:
		return "", fmt.Errorf("invalid misfire policy %q", value)
	}
}
