package freshguard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const tavilyExtractEndpoint = "https://api.tavily.com/extract"

type DirectURLVerifier struct {
	client   *http.Client
	apiKey   string
	endpoint string
	urls     []string
	scorer   scoreFunc
	now      func() time.Time
}

func NewDirectURLVerifier(client *http.Client, apiKey string, urls []string, scorer scoreFunc) *DirectURLVerifier {
	if client == nil {
		client = http.DefaultClient
	}
	if scorer == nil {
		scorer = Score
	}
	return &DirectURLVerifier{
		client: client, apiKey: strings.TrimSpace(apiKey), endpoint: tavilyExtractEndpoint,
		urls: normalizeTrustedURLs(urls), scorer: scorer, now: time.Now,
	}
}

func (v *DirectURLVerifier) Verify(ctx context.Context, claims []Claim) ([]ClaimResult, error) {
	if len(v.urls) == 0 {
		return nil, fmt.Errorf("%w: no valid trusted URLs configured", ErrVerificationUnavailable)
	}
	if len(claims) > 3 {
		claims = claims[:3]
	}
	results := make([]ClaimResult, 0, len(claims))
	for _, claim := range claims {
		evidence, err := v.extract(ctx, claim)
		if err != nil {
			return nil, err
		}
		results = append(results, v.scorer(claim, evidence, v.now()))
	}
	return results, nil
}

func (v *DirectURLVerifier) extract(ctx context.Context, claim Claim) ([]Evidence, error) {
	payload, err := json.Marshal(map[string]any{
		"urls": v.urls, "query": strings.TrimSpace(claim.Text),
		"extract_depth": "advanced", "chunks_per_source": 3,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: encode Tavily extract request", ErrVerificationUnavailable)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%w: create Tavily extract request", ErrVerificationUnavailable)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.apiKey)
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: Tavily extract request failed: %v", ErrVerificationUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("%w: Tavily extract returned %s", ErrVerificationUnavailable, resp.Status)
	}
	var response struct {
		Results []struct {
			URL        string `json:"url"`
			RawContent string `json:"raw_content"`
		} `json:"results"`
		FailedResults []any `json:"failed_results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("%w: decode Tavily extract response: %v", ErrVerificationUnavailable, err)
	}
	if len(response.Results) == 0 && len(response.FailedResults) > 0 {
		return nil, fmt.Errorf("%w: Tavily could not extract trusted URLs", ErrVerificationUnavailable)
	}
	claimText := normalizeComparableText(claim.Text)
	evidence := make([]Evidence, 0, len(response.Results))
	for _, result := range response.Results {
		content := strings.TrimSpace(result.RawContent)
		if content == "" {
			continue
		}
		supports := claimText != "" && strings.Contains(normalizeComparableText(content), claimText)
		snippet := content
		if len(snippet) > 1000 {
			snippet = snippet[:1000]
		}
		evidence = append(evidence, Evidence{
			URL: result.URL, Title: "Authoritative source extract", Snippet: snippet,
			Supports: supports, Entailment: 1,
		})
	}
	return evidence, nil
}

func normalizeTrustedURLs(values []string) []string {
	urls := make([]string, 0, min(3, len(values)))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
			continue
		}
		parsed.Fragment = ""
		normalized := parsed.String()
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		urls = append(urls, normalized)
		if len(urls) == 3 {
			break
		}
	}
	return urls
}

func normalizeComparableText(value string) string {
	var builder strings.Builder
	space := true
	for _, char := range strings.ToLower(value) {
		if unicode.IsLetter(char) || unicode.IsNumber(char) {
			builder.WriteRune(char)
			space = false
		} else if !space {
			builder.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(builder.String())
}

var _ Verifier = (*DirectURLVerifier)(nil)
