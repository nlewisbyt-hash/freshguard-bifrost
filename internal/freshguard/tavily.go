package freshguard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const tavilySearchEndpoint = "https://api.tavily.com/search"

type scoreFunc func(Claim, []Evidence, time.Time) ClaimResult

type TavilyVerifier struct {
	client   *http.Client
	apiKey   string
	endpoint string
	scorer   scoreFunc
	now      func() time.Time
}

func NewTavilyVerifier(client *http.Client, apiKey string, scorer scoreFunc) *TavilyVerifier {
	if client == nil {
		client = http.DefaultClient
	}
	if scorer == nil {
		scorer = Score
	}
	return &TavilyVerifier{
		client:   client,
		apiKey:   strings.TrimSpace(apiKey),
		endpoint: tavilySearchEndpoint,
		scorer:   scorer,
		now:      time.Now,
	}
}

func (v *TavilyVerifier) Verify(ctx context.Context, claims []Claim) ([]ClaimResult, error) {
	if len(claims) > 3 {
		claims = claims[:3]
	}
	results := make([]ClaimResult, 0, len(claims))
	for _, claim := range claims {
		evidence, err := v.search(ctx, claim.Text)
		if err != nil {
			return nil, err
		}
		results = append(results, v.scorer(claim, evidence, v.now()))
	}
	return results, nil
}

func (v *TavilyVerifier) search(ctx context.Context, query string) ([]Evidence, error) {
	payload, err := json.Marshal(map[string]any{
		"query":          query,
		"search_depth":   "advanced",
		"max_results":    3,
		"include_answer": false,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: encode Tavily request", ErrVerificationUnavailable)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%w: create Tavily request", ErrVerificationUnavailable)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.apiKey)

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: Tavily request failed: %v", ErrVerificationUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("%w: Tavily returned %s", ErrVerificationUnavailable, resp.Status)
	}

	var response struct {
		Results []struct {
			URL           string  `json:"url"`
			Title         string  `json:"title"`
			Content       string  `json:"content"`
			PublishedDate string  `json:"published_date"`
			Score         float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("%w: decode Tavily response: %v", ErrVerificationUnavailable, err)
	}

	evidence := make([]Evidence, 0, len(response.Results))
	for _, result := range response.Results {
		publishedAt := parsePublishedDate(result.PublishedDate)
		entailment := result.Score
		if entailment < 0 {
			entailment = 0
		}
		if entailment > 1 {
			entailment = 1
		}
		evidence = append(evidence, Evidence{
			URL:         result.URL,
			Title:       result.Title,
			Snippet:     result.Content,
			PublishedAt: publishedAt,
			Supports:    result.Score >= 0.5,
			Entailment:  entailment,
		})
	}
	return evidence, nil
}

func parsePublishedDate(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
