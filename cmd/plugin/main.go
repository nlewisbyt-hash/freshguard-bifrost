package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/maximhq/bifrost/core/schemas"
	freshguardbifrost "github.com/phishguard/freshguard-bifrost"
	"github.com/phishguard/freshguard-bifrost/internal/freshguard"
)

var (
	pluginMu sync.RWMutex
	instance *freshguardbifrost.Plugin
)

func Init(config any) error {
	raw, err := configMap(config)
	if err != nil {
		return err
	}
	raw["api_key"] = strings.TrimSpace(os.Getenv("TAVILY_API_KEY"))
	cfg, err := freshguard.ParseConfig(raw)
	if err != nil {
		return fmt.Errorf("freshguard config: %w", err)
	}

	extractorEndpoint := stringValue(raw, "extractor_endpoint", "https://api.openai.com/v1/chat/completions")
	extractorModel := stringValue(raw, "extractor_model", "gpt-4o-mini")
	extractorKey := strings.TrimSpace(os.Getenv("EXTRACTOR_API_KEY"))
	if cfg.Enabled && extractorKey == "" {
		return fmt.Errorf("freshguard config: EXTRACTOR_API_KEY is required when enabled")
	}
	client := &http.Client{Timeout: cfg.Timeout}
	trustedURLs := stringSliceValue(raw, "trusted_urls")
	var verifier freshguard.Verifier
	if len(trustedURLs) > 0 {
		judge := freshguard.NewJSONEntailmentJudge(client, extractorEndpoint, extractorModel, extractorKey)
		verifier = freshguard.NewDirectURLVerifierWithJudge(client, cfg.APIKey, trustedURLs, freshguard.Score, judge)
	} else {
		verifier = freshguard.NewTavilyVerifierWithDomains(client, cfg.APIKey, stringSliceValue(raw, "trusted_domains"), freshguard.Score)
	}
	created := freshguardbifrost.New(
		cfg,
		freshguard.NewJSONExtractor(client, extractorEndpoint, extractorModel, extractorKey),
		verifier,
	)
	pluginMu.Lock()
	instance = created
	pluginMu.Unlock()
	return nil
}

func GetName() string { return "freshguard" }

func PreRequestHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) error {
	if current := currentPlugin(); current != nil {
		return current.PreRequestHook(ctx, req)
	}
	return nil
}

func PreLLMHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) (*schemas.BifrostRequest, *schemas.LLMPluginShortCircuit, error) {
	if current := currentPlugin(); current != nil {
		return current.PreLLMHook(ctx, req)
	}
	return req, nil, nil
}

func PostLLMHook(ctx *schemas.BifrostContext, resp *schemas.BifrostResponse, bifrostErr *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError, error) {
	if current := currentPlugin(); current != nil {
		return current.PostLLMHook(ctx, resp, bifrostErr)
	}
	return resp, bifrostErr, nil
}

func Cleanup() error {
	pluginMu.Lock()
	defer pluginMu.Unlock()
	if instance == nil {
		return nil
	}
	err := instance.Cleanup()
	instance = nil
	return err
}

func currentPlugin() *freshguardbifrost.Plugin {
	pluginMu.RLock()
	defer pluginMu.RUnlock()
	return instance
}

func configMap(config any) (map[string]any, error) {
	if config == nil {
		return map[string]any{}, nil
	}
	raw, ok := config.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("freshguard config must be an object, got %T", config)
	}
	copy := make(map[string]any, len(raw)+1)
	for key, value := range raw {
		copy[key] = value
	}
	return copy, nil
}

func stringValue(raw map[string]any, key, fallback string) string {
	if value, ok := raw[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func stringSliceValue(raw map[string]any, key string) []string {
	appendValue := func(values []string, value any) []string {
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return values
		}
		return append(values, strings.TrimSpace(text))
	}
	values := []string{}
	switch rawValues := raw[key].(type) {
	case []string:
		for _, value := range rawValues {
			values = appendValue(values, value)
		}
	case []any:
		for _, value := range rawValues {
			values = appendValue(values, value)
		}
	}
	return values
}
