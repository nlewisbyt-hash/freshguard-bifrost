package freshguard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Extractor interface {
	Extract(ctx context.Context, text string, limit int) ([]Claim, error)
}

type JSONExtractor struct {
	client   *http.Client
	endpoint string
	model    string
	apiKey   string
}

func NewJSONExtractor(client *http.Client, endpoint, model, apiKey string) *JSONExtractor {
	if client == nil {
		client = http.DefaultClient
	}
	return &JSONExtractor{
		client:   client,
		endpoint: strings.TrimSpace(endpoint),
		model:    strings.TrimSpace(model),
		apiKey:   strings.TrimSpace(apiKey),
	}
}

func (e *JSONExtractor) Extract(ctx context.Context, text string, limit int) ([]Claim, error) {
	text = strings.TrimSpace(text)
	if text == "" || limit <= 0 {
		return []Claim{}, nil
	}

	requestBody := map[string]any{
		"model": e.model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "Extract only objective, time-sensitive atomic claims. Return strict JSON as {\"claims\":[{\"text\":\"...\",\"domain\":\"pricing|api_version|regulation|company|person|schedule|other\"}]}. Return an empty array for opinions or timeless statements.",
			},
			{"role": "user", "content": text},
		},
		"response_format": map[string]string{"type": "json_object"},
		"temperature":     0,
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("encode extraction request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create extraction request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("extract claims: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("extract claims: endpoint returned %s", resp.Status)
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return nil, fmt.Errorf("decode extraction response: %w", err)
	}
	if len(completion.Choices) == 0 {
		return nil, fmt.Errorf("decode extraction response: missing choices")
	}

	var result struct {
		Claims []Claim `json:"claims"`
	}
	decoder := json.NewDecoder(strings.NewReader(completion.Choices[0].Message.Content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode extracted claims: %w", err)
	}

	allowedDomains := map[string]bool{
		"pricing": true, "api_version": true, "regulation": true,
		"company": true, "person": true, "schedule": true, "other": true,
	}
	claims := make([]Claim, 0, min(limit, len(result.Claims)))
	seen := make(map[string]struct{}, len(result.Claims))
	for _, claim := range result.Claims {
		claim.Text = strings.TrimSpace(claim.Text)
		claim.Domain = strings.TrimSpace(claim.Domain)
		if claim.Text == "" {
			return nil, fmt.Errorf("extracted claim text must not be empty")
		}
		if !allowedDomains[claim.Domain] {
			return nil, fmt.Errorf("unsupported claim domain %q", claim.Domain)
		}
		key := strings.ToLower(strings.Join(strings.Fields(claim.Text), " "))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		claims = append(claims, claim)
		if len(claims) == limit {
			break
		}
	}
	return claims, nil
}
