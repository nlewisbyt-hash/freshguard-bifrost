package freshguard

import (
	"testing"
	"time"
)

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := ParseConfig(nil)
	if err != nil {
		t.Fatalf("ParseConfig(nil) error = %v", err)
	}

	if cfg.Enabled {
		t.Fatal("default config must be disabled until credentials are supplied")
	}
	if cfg.Policy != PolicyWarn {
		t.Fatalf("Policy = %q, want %q", cfg.Policy, PolicyWarn)
	}
	if cfg.Threshold != 60 {
		t.Fatalf("Threshold = %d, want 60", cfg.Threshold)
	}
	if cfg.MaxClaims != 3 || cfg.MaxQueries != 3 {
		t.Fatalf("limits = (%d, %d), want (3, 3)", cfg.MaxClaims, cfg.MaxQueries)
	}
	if cfg.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %s, want 5s", cfg.Timeout)
	}
}

func TestParseConfigExplicitValues(t *testing.T) {
	cfg, err := ParseConfig(map[string]any{
		"enabled":     true,
		"api_key":     "tvly-test",
		"policy":      "block",
		"threshold":   75,
		"max_claims":  2,
		"max_queries": 1,
		"timeout":     "3s",
	})
	if err != nil {
		t.Fatalf("ParseConfig(explicit) error = %v", err)
	}

	if !cfg.Enabled || cfg.APIKey != "tvly-test" || cfg.Policy != PolicyBlock {
		t.Fatalf("identity fields = %#v", cfg)
	}
	if cfg.Threshold != 75 || cfg.MaxClaims != 2 || cfg.MaxQueries != 1 || cfg.Timeout != 3*time.Second {
		t.Fatalf("explicit config = %#v", cfg)
	}
}

func TestParseConfigAcceptsIntegralJSONNumbers(t *testing.T) {
	cfg, err := ParseConfig(map[string]any{
		"threshold":   float64(75),
		"max_claims":  float64(2),
		"max_queries": float64(1),
	})
	if err != nil {
		t.Fatalf("ParseConfig(JSON numbers) error = %v", err)
	}
	if cfg.Threshold != 75 || cfg.MaxClaims != 2 || cfg.MaxQueries != 1 {
		t.Fatalf("JSON number config = %#v", cfg)
	}
}

func TestParseConfigRejectsFractionalJSONNumbers(t *testing.T) {
	_, err := ParseConfig(map[string]any{"threshold": 60.5})
	if err == nil {
		t.Fatal("ParseConfig accepted a fractional threshold")
	}
}

func TestParseConfigRejectsUnknownPolicy(t *testing.T) {
	_, err := ParseConfig(map[string]any{"policy": "silently-rewrite"})
	if err == nil {
		t.Fatal("ParseConfig accepted an unknown policy")
	}
}

func TestParseConfigRejectsThresholdOutsideRange(t *testing.T) {
	for _, threshold := range []int{-1, 101} {
		t.Run(string(rune(threshold+1024)), func(t *testing.T) {
			_, err := ParseConfig(map[string]any{"threshold": threshold})
			if err == nil {
				t.Fatalf("ParseConfig accepted threshold %d", threshold)
			}
		})
	}
}

func TestParseConfigRequiresAPIKeyWhenEnabled(t *testing.T) {
	_, err := ParseConfig(map[string]any{"enabled": true})
	if err == nil {
		t.Fatal("ParseConfig enabled verification without an API key")
	}
}
