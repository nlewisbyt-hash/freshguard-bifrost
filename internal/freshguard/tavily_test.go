package freshguard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTavilyMapsEvidenceAndFormsQuery(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tavily-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		query = request.Query
		_, _ = w.Write([]byte(`{"results":[{"url":"https://example.com/current","title":"Current API","content":"The API is v3","published_date":"2026-11-01T00:00:00Z","score":0.9}]}`))
	}))
	defer server.Close()

	verifier := NewTavilyVerifier(server.Client(), "tavily-key", Score)
	verifier.endpoint = server.URL
	results, err := verifier.Verify(context.Background(), []Claim{{Text: "The API is v3", Domain: "api_version"}})
	if err != nil {
		t.Fatal(err)
	}
	if query != "The API is v3" {
		t.Fatalf("query = %q", query)
	}
	if len(results) != 1 || len(results[0].Evidence) != 1 {
		t.Fatalf("results = %#v", results)
	}
	evidence := results[0].Evidence[0]
	if evidence.URL != "https://example.com/current" || evidence.Title != "Current API" || evidence.Snippet != "The API is v3" {
		t.Fatalf("evidence = %#v", evidence)
	}
	if evidence.PublishedAt.IsZero() || !evidence.Supports || evidence.Entailment != 0.9 {
		t.Fatalf("mapped scores = %#v", evidence)
	}
}

func TestTavilyCapsQueriesAtThree(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()
	verifier := NewTavilyVerifier(server.Client(), "key", Score)
	verifier.endpoint = server.URL

	claims := []Claim{{Text: "one"}, {Text: "two"}, {Text: "three"}, {Text: "four"}}
	results, err := verifier.Verify(context.Background(), claims)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || len(results) != 3 {
		t.Fatalf("calls = %d, results = %d", calls.Load(), len(results))
	}
	for _, result := range results {
		if result.Status != "unverified" {
			t.Fatalf("empty evidence status = %q", result.Status)
		}
	}
}

func TestTavilyFailureSemantics(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		client     func(*httptest.Server) *http.Client
		wantErr    bool
		wantStatus string
	}{
		{
			name: "rate limit",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
			},
			wantErr: true,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
		{
			name: "malformed JSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"results":`))
			},
			wantErr: true,
		},
		{
			name: "empty results",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"results":[]}`))
			},
			wantStatus: "unverified",
		},
		{
			name: "timeout",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				time.Sleep(50 * time.Millisecond)
				_, _ = w.Write([]byte(`{"results":[]}`))
			},
			client: func(server *httptest.Server) *http.Client {
				return &http.Client{Transport: server.Client().Transport, Timeout: 5 * time.Millisecond}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := server.Client()
			if tt.client != nil {
				client = tt.client(server)
			}
			verifier := NewTavilyVerifier(client, "key", Score)
			verifier.endpoint = server.URL
			results, err := verifier.Verify(context.Background(), []Claim{{Text: "current fact"}})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrVerificationUnavailable) {
				t.Fatalf("error = %v, want ErrVerificationUnavailable", err)
			}
			if tt.wantStatus != "" && (len(results) != 1 || results[0].Status != tt.wantStatus) {
				t.Fatalf("results = %#v", results)
			}
		})
	}
}

func TestTavilyRejectsMalformedPublishedDateWithoutFailingVerification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"url":"https://example.com","title":"Source","content":"Snippet","published_date":"not-a-date","score":0.8}]}`))
	}))
	defer server.Close()
	verifier := NewTavilyVerifier(server.Client(), "key", Score)
	verifier.endpoint = server.URL
	results, err := verifier.Verify(context.Background(), []Claim{{Text: "fact"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].Evidence) != 1 || !results[0].Evidence[0].PublishedAt.IsZero() {
		t.Fatalf("results = %#v", results)
	}
}

func TestTavilyDoesNotLeakAPIKeyInErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	verifier := NewTavilyVerifier(server.Client(), "super-secret", Score)
	verifier.endpoint = server.URL
	_, err := verifier.Verify(context.Background(), []Claim{{Text: "fact"}})
	if err == nil || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("unsafe error = %v", err)
	}
}
