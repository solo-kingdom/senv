package session

import (
	"testing"
	"time"
)

func TestParseTimeout(t *testing.T) {
	tests := []struct {
		input       string
		expectType  TimeoutType
		expectValue time.Duration
		expectNil   bool
		expectError bool
	}{
		{"30m", TimeoutDuration, 30 * time.Minute, false, false},
		{"8h", TimeoutDuration, 8 * time.Hour, false, false},
		{"1d", TimeoutDuration, 24 * time.Hour, false, false},
		{"7d", TimeoutDuration, 7 * 24 * time.Hour, false, false},
		{"1y", TimeoutDuration, 365 * 24 * time.Hour, false, false},
		{"restart", TimeoutRestart, 0, false, false},
		{"never", TimeoutNever, 0, false, false},
		{"false", "", 0, true, false},
		{"disabled", "", 0, true, false},
		{"invalid", "", 0, false, true},
		{"10s", "", 0, false, true}, // Less than 1 minute
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			timeout, err := ParseTimeout(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.expectNil {
				if timeout != nil {
					t.Errorf("expected nil but got %+v", timeout)
				}
				return
			}

			if timeout == nil {
				t.Errorf("expected non-nil timeout")
				return
			}

			if timeout.Type != tt.expectType {
				t.Errorf("expected type %s but got %s", tt.expectType, timeout.Type)
			}

			if tt.expectType == TimeoutDuration && timeout.Value != tt.expectValue {
				t.Errorf("expected value %v but got %v", tt.expectValue, timeout.Value)
			}
		})
	}
}

func TestTimeoutString(t *testing.T) {
	tests := []struct {
		timeout *SessionTimeout
		expect  string
	}{
		{&SessionTimeout{Type: TimeoutDuration, Value: 8 * time.Hour}, "8h"},
		{&SessionTimeout{Type: TimeoutDuration, Value: 24 * time.Hour}, "1d"},
		{&SessionTimeout{Type: TimeoutDuration, Value: 7 * 24 * time.Hour}, "7d"},
		{&SessionTimeout{Type: TimeoutRestart}, "until restart"},
		{&SessionTimeout{Type: TimeoutNever}, "never"},
	}

	for _, tt := range tests {
		t.Run(tt.expect, func(t *testing.T) {
			result := tt.timeout.String()
			if result != tt.expect {
				t.Errorf("expected %s but got %s", tt.expect, result)
			}
		})
	}
}
