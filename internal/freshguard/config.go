package freshguard

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

func ParseConfig(raw map[string]any) (Config, error) {
	cfg := Config{
		Policy:     PolicyWarn,
		Threshold:  60,
		MaxClaims:  3,
		MaxQueries: 3,
		Timeout:    5 * time.Second,
	}

	if raw == nil {
		return cfg, nil
	}

	if value, ok := raw["enabled"]; ok {
		enabled, ok := value.(bool)
		if !ok {
			return Config{}, fmt.Errorf("enabled must be a boolean")
		}
		cfg.Enabled = enabled
	}
	if value, ok := raw["api_key"]; ok {
		apiKey, ok := value.(string)
		if !ok {
			return Config{}, fmt.Errorf("api_key must be a string")
		}
		cfg.APIKey = strings.TrimSpace(apiKey)
	}
	if value, ok := raw["policy"]; ok {
		policy, ok := value.(string)
		if !ok {
			return Config{}, fmt.Errorf("policy must be a string")
		}
		cfg.Policy = Policy(policy)
	}
	if value, ok := raw["threshold"]; ok {
		threshold, ok := configInteger(value)
		if !ok {
			return Config{}, fmt.Errorf("threshold must be an integer")
		}
		cfg.Threshold = threshold
	}
	if value, ok := raw["max_claims"]; ok {
		maxClaims, ok := configInteger(value)
		if !ok {
			return Config{}, fmt.Errorf("max_claims must be an integer")
		}
		cfg.MaxClaims = maxClaims
	}
	if value, ok := raw["max_queries"]; ok {
		maxQueries, ok := configInteger(value)
		if !ok {
			return Config{}, fmt.Errorf("max_queries must be an integer")
		}
		cfg.MaxQueries = maxQueries
	}
	if value, ok := raw["timeout"]; ok {
		timeoutText, ok := value.(string)
		if !ok {
			return Config{}, fmt.Errorf("timeout must be a duration string")
		}
		timeout, err := time.ParseDuration(timeoutText)
		if err != nil {
			return Config{}, fmt.Errorf("timeout: %w", err)
		}
		cfg.Timeout = timeout
	}

	if cfg.Policy != PolicyAllow && cfg.Policy != PolicyWarn && cfg.Policy != PolicyBlock {
		return Config{}, fmt.Errorf("unknown policy %q", cfg.Policy)
	}
	if cfg.Threshold < 0 || cfg.Threshold > 100 {
		return Config{}, fmt.Errorf("threshold must be between 0 and 100")
	}
	if cfg.MaxClaims < 1 || cfg.MaxQueries < 1 {
		return Config{}, fmt.Errorf("max_claims and max_queries must be positive")
	}
	if cfg.Timeout <= 0 {
		return Config{}, fmt.Errorf("timeout must be positive")
	}
	if cfg.Enabled && cfg.APIKey == "" {
		return Config{}, fmt.Errorf("api_key is required when enabled")
	}

	return cfg, nil
}

func configInteger(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, true
	case int32:
		return int(number), true
	case int64:
		converted := int(number)
		return converted, int64(converted) == number
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number {
			return 0, false
		}
		converted := int(number)
		return converted, float64(converted) == number
	case json.Number:
		parsed, err := number.Int64()
		if err != nil {
			return 0, false
		}
		converted := int(parsed)
		return converted, int64(converted) == parsed
	default:
		return 0, false
	}
}
