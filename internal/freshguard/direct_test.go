package freshguard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDirectURLVerifierExtractsAndMatchesNormalizedClaim(t *testing.T) {
	var request struct {
		URLs            []string `json:"urls"`
		Query           string   `json:"query"`
		ExtractDepth    string   `json:"extract_depth"`
		ChunksPerSource int      `json:"chunks_per_source"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"results":[{"url":"https://docs.example.com/page","raw_content":"The API — costs $2 per million tokens."}],"failed_results":[]}`))
	}))
	defer server.Close()

	verifier := NewDirectURLVerifier(server.Client(), "secret", []string{"https://docs.example.com/page"}, Score)
	verifier.endpoint = server.URL
	claim := Claim{Text: "the api costs $2 per million tokens", Domain: "pricing"}
	results, err := verifier.Verify(context.Background(), []Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(request.URLs, ",") != "https://docs.example.com/page" || request.Query != claim.Text || request.ExtractDepth != "advanced" || request.ChunksPerSource != 3 {
		t.Fatalf("request = %#v", request)
	}
	if len(results) != 1 || results[0].Status != "verified" || len(results[0].Evidence) != 1 || !results[0].Evidence[0].Supports {
		t.Fatalf("results = %#v", results)
	}
}

func TestDirectURLVerifierDoesNotSupportContradictoryQualifier(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"url":"https://event.example/prizes","raw_content":"The Tavily track has 50,000 API credits distributed among the top three winning projects."}]}`))
	}))
	defer server.Close()
	verifier := NewDirectURLVerifier(server.Client(), "key", []string{"https://event.example/prizes"}, Score)
	verifier.endpoint = server.URL

	results, err := verifier.Verify(context.Background(), []Claim{{Text: "The Tavily track gives 50,000 API credits to every participant."}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "stale" || results[0].Evidence[0].Supports {
		t.Fatalf("results = %#v", results)
	}
}

func TestDirectURLVerifierEmptyExtractionIsUnverified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()
	verifier := NewDirectURLVerifier(server.Client(), "key", []string{"https://docs.example/page"}, Score)
	verifier.endpoint = server.URL
	results, err := verifier.Verify(context.Background(), []Claim{{Text: "claim"}})
	if err != nil || len(results) != 1 || results[0].Status != "unverified" {
		t.Fatalf("results = %#v, err = %v", results, err)
	}
}

func TestDirectURLVerifierRejectsInvalidOrFailedExtraction(t *testing.T) {
	t.Run("invalid URLs", func(t *testing.T) {
		verifier := NewDirectURLVerifier(http.DefaultClient, "key", []string{"http://insecure.example", "://bad"}, Score)
		_, err := verifier.Verify(context.Background(), []Claim{{Text: "claim"}})
		if !errors.Is(err, ErrVerificationUnavailable) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("API failure without key leak", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()
		verifier := NewDirectURLVerifier(server.Client(), "super-secret", []string{"https://docs.example/page"}, Score)
		verifier.endpoint = server.URL
		_, err := verifier.Verify(context.Background(), []Claim{{Text: "claim"}})
		if !errors.Is(err, ErrVerificationUnavailable) || strings.Contains(err.Error(), "super-secret") {
			t.Fatalf("error = %v", err)
		}
	})
}
