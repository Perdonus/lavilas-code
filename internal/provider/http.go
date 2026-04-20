package provider

import (
	"strconv"
	"strings"
	"time"
)

func ParseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	retryAt, err := time.Parse(time.RFC1123, value)
	if err != nil {
		return 0
	}
	delay := retryAt.Sub(now)
	if delay < 0 {
		return 0
	}
	return delay
}
