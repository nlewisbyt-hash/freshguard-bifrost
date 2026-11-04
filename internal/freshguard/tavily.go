package freshguard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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
	domains  []string
}

func NewTavilyVerifier(client *http.Client, apiKey string, scorer scoreFunc) *TavilyVerifier {
	return NewTavilyVerifierWithDomains(client, apiKey, nil, scorer)
}

func NewTavilyVerifierWithDomains(client *http.Client, apiKey string, domains []string, scorer scoreFunc) *TavilyVerifier {
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
		domains:  normalizeDomains(domains),
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
	query = "Determine whether the claim is true according to current sources. Your answer MUST begin with exactly YES: or NO:. Claim: " + strings.TrimSpace(query)
	requestBody := map[string]any{
		"query":          query,
		"search_depth":   "advanced",
		"max_results":    3,
		"include_answer": "advanced",
	}
	if len(v.domains) > 0 {
		requestBody["include_domains"] = v.domains
	}
	payload, err := json.Marshal(requestBody)
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
		Answer  string `json:"answer"`
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
	supports := answerSupportsClaim(response.Answer)
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
			Supports:    supports,
			Entailment:  entailment,
		})
	}
	return evidence, nil
}

func normalizeDomains(values []string) []string {
	domains := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !strings.Contains(value, "://") {
			value = "https://" + value
		}
		parsed, err := url.Parse(value)
		if err != nil {
			continue
		}
		host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		host = strings.TrimPrefix(host, "www.")
		if !validDomain(host) {
			continue
		}
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		domains = append(domains, host)
	}
	return domains
}

func validDomain(host string) bool {
	if host == "" || !strings.Contains(host, ".") || net.ParseIP(host) != nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func answerSupportsClaim(answer string) bool {
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "yes" || strings.HasPrefix(answer, "yes ") || strings.HasPrefix(answer, "yes:") || strings.HasPrefix(answer, "yes;") || strings.HasPrefix(answer, "yes,") || strings.HasPrefix(answer, "yes.")
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
