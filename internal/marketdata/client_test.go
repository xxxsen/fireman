package marketdata

import (
	"testing"
	"time"
)

func TestEnvDuration(t *testing.T) {
	t.Setenv("MARKET_PROVIDER_RESOLVE_TIMEOUT", "")

	tests := []struct {
		name     string
		env      string
		fallback time.Duration
		want     time.Duration
	}{
		{name: "empty uses fallback", env: "", fallback: 90 * time.Second, want: 90 * time.Second},
		{name: "plain seconds", env: "45", fallback: 90 * time.Second, want: 45 * time.Second},
		{name: "duration string", env: "90s", fallback: 20 * time.Second, want: 90 * time.Second},
		{name: "invalid uses fallback", env: "nope", fallback: 90 * time.Second, want: 90 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env == "" {
				t.Setenv("MARKET_PROVIDER_RESOLVE_TIMEOUT", "")
			} else {
				t.Setenv("MARKET_PROVIDER_RESOLVE_TIMEOUT", tt.env)
			}
			if got := envDuration("MARKET_PROVIDER_RESOLVE_TIMEOUT", tt.fallback); got != tt.want {
				t.Fatalf("envDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveTimeoutDefault(t *testing.T) {
	t.Setenv("MARKET_PROVIDER_RESOLVE_TIMEOUT", "")
	if got := resolveTimeout(); got != 90*time.Second {
		t.Fatalf("resolveTimeout() = %v, want 90s", got)
	}
}
