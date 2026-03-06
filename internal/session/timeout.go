package session

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var durationRegex = regexp.MustCompile(`^(\d+)(ns|us|ms|s|m|h|d|y)$`)

// ParseTimeout parses a timeout string into a SessionTimeout
// Supports formats:
//   - Duration: 30m, 8h, 1d, 1y
//   - Special: restart, never
//   - Disable: false, disabled
func ParseTimeout(input string) (*SessionTimeout, error) {
	input = strings.ToLower(strings.TrimSpace(input))

	// Handle special types
	switch input {
	case "restart", "until-restart", "until_restart":
		return &SessionTimeout{Type: TimeoutRestart}, nil
	case "never", "infinite", "forever":
		return &SessionTimeout{Type: TimeoutNever}, nil
	case "false", "disabled", "disable", "off":
		return nil, nil // Indicates cache is disabled
	}

	// Parse duration
	matches := durationRegex.FindStringSubmatch(input)
	if matches == nil {
		return nil, fmt.Errorf("invalid timeout format: %s (supported: 30m, 8h, 1d, 1y, restart, never)", input)
	}

	value, _ := strconv.ParseInt(matches[1], 10, 64)
	unit := matches[2]

	var duration time.Duration
	switch unit {
	case "ns":
		duration = time.Duration(value) * time.Nanosecond
	case "us":
		duration = time.Duration(value) * time.Microsecond
	case "ms":
		duration = time.Duration(value) * time.Millisecond
	case "s":
		duration = time.Duration(value) * time.Second
	case "m":
		duration = time.Duration(value) * time.Minute
	case "h":
		duration = time.Duration(value) * time.Hour
	case "d":
		duration = time.Duration(value) * 24 * time.Hour
	case "y":
		duration = time.Duration(value) * 365 * 24 * time.Hour
	default:
		return nil, fmt.Errorf("unknown time unit: %s", unit)
	}

	if duration < time.Minute {
		return nil, fmt.Errorf("timeout must be at least 1 minute")
	}

	return &SessionTimeout{
		Type:  TimeoutDuration,
		Value: duration,
	}, nil
}

// String returns a human-readable representation of the timeout
func (st *SessionTimeout) String() string {
	switch st.Type {
	case TimeoutRestart:
		return "until restart"
	case TimeoutNever:
		return "never"
	case TimeoutDuration:
		return formatDuration(st.Value)
	default:
		return "unknown"
	}
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	// Convert to largest unit possible
	if d >= 365*24*time.Hour {
		years := int(d / (365 * 24 * time.Hour))
		return fmt.Sprintf("%dy", years)
	}
	if d >= 24*time.Hour {
		days := int(d / (24 * time.Hour))
		return fmt.Sprintf("%dd", days)
	}
	if d >= time.Hour {
		hours := int(d / time.Hour)
		minutes := int((d % time.Hour) / time.Minute)
		if minutes > 0 {
			return fmt.Sprintf("%dh%dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}
	if d >= time.Minute {
		minutes := int(d / time.Minute)
		return fmt.Sprintf("%dm", minutes)
	}
	return d.String()
}
