package env

import (
	"os"
	"testing"
	"time"
)

func TestGetString(t *testing.T) {
	// Test with existing environment variable
	_ = os.Setenv("TEST_KEY", "test_value")
	result := GetString("TEST_KEY", "default_value")
	if result != "test_value" {
		t.Errorf("Expected 'test_value', got '%s'", result)
	}

	// Test with non-existing environment variable
	result = GetString("NON_EXISTING_KEY", "default_value")
	if result != "default_value" {
		t.Errorf("Expected 'default_value', got '%s'", result)
	}

	// Test with empty environment variable
	_ = os.Setenv("EMPTY_KEY", "")
	result = GetString("EMPTY_KEY", "default_value")
	if result != "default_value" {
		t.Errorf("Expected 'default_value', got '%s'", result)
	}
}

func TestGetInt(t *testing.T) {
	// Test with existing environment variable
	_ = os.Setenv("INT_KEY", "1234")
	result := GetInt("INT_KEY", 8080)
	if result != 1234 {
		t.Errorf("Expected 1234, got %d", result)
	}

	// Test with non-existing environment variable
	result = GetInt("NON_EXISTING_INT_KEY", 8080)
	if result != 8080 {
		t.Errorf("Expected 8080, got %d", result)
	}

	// Test with invalid integer value
	_ = os.Setenv("INVALID_INT_KEY", "not_a_number")
	result = GetInt("INVALID_INT_KEY", 8080)
	if result != 8080 {
		t.Errorf("Expected 8080, got %d", result)
	}
}

func TestGetTimeDuration(t *testing.T) {
	tests := []struct {
		name         string
		envKey       string
		envValue     string
		defaultValue time.Duration
		want         time.Duration
	}{
		{
			name:         "valid duration from env",
			envKey:       "TD_VALID",
			envValue:     "2h30m",
			defaultValue: time.Hour,
			want:         2*time.Hour + 30*time.Minute,
		},
		{
			name:         "missing env key uses default",
			envKey:       "TD_MISSING_XYZ",
			envValue:     "",
			defaultValue: 5 * time.Minute,
			want:         5 * time.Minute,
		},
		{
			name:         "invalid duration uses default",
			envKey:       "TD_INVALID",
			envValue:     "not-a-duration",
			defaultValue: 10 * time.Second,
			want:         10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				_ = os.Setenv(tt.envKey, tt.envValue)
				defer func() {
					if err := os.Unsetenv(tt.envKey); err != nil {
						t.Errorf("os.Unsetenv(%q): %v", tt.envKey, err)
					}
				}()
			} else {
				if err := os.Unsetenv(tt.envKey); err != nil {
					t.Errorf("os.Unsetenv(%q): %v", tt.envKey, err)
				}
			}

			got := GetTimeDuration(tt.envKey, tt.defaultValue)
			if got != tt.want {
				t.Errorf("GetTimeDuration(%q) = %v, want %v", tt.envKey, got, tt.want)
			}
		})
	}
}
