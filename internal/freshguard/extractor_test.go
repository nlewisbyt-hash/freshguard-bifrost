package freshguard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONExtractor(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		response   string
		limit      int
		want       []Claim
		wantErr    bool
		wantCalled bool
	}{
		{
			name:       "valid atomic claims",
			text:       "The API is currently v3 and costs $10.",
			response:   `{"choices":[{"message":{"content":"{\"claims\":[{\"text\":\"The API is currently v3\",\"domain\":\"api_version\"},{\"text\":\"The API costs $10\",\"domain\":\"pricing\"}]}"}}]}`,
			limit:      3,
			want:       []Claim{{Text: "The API is currently v3", Domain: "api_version"}, {Text: "The API costs $10", Domain: "pricing"}},
			wantCalled: true,
		},
		{
			name:       "malformed JSON",
			text:       "Current fact",
			response:   `{"choices":[{"message":{"content":"not-json"}}]}`,
			limit:      3,
			wantErr:    true,
			wantCalled: true,
		},
		{
			name:       "deduplicates normalized claims",
			text:       "Repeated current fact",
			response:   `{"choices":[{"message":{"content":"{\"claims\":[{\"text\":\"  API V3 is current. \",\"domain\":\"api_version\"},{\"text\":\"api v3 is current.\",\"domain\":\"api_version\"}]}"}}]}`,
			limit:      3,
			want:       []Claim{{Text: "API V3 is current.", Domain: "api_version"}},
			wantCalled: true,
		},
		{
			name:       "rejects unsupported subjective domain",
			text:       "This is the best API.",
			response:   `{"choices":[{"message":{"content":"{\"claims\":[{\"text\":\"This is the best API\",\"domain\":\"opinion\"}]}"}}]}`,
			limit:      3,
			wantErr:    true,
			wantCalled: true,
		},
		{
			name:       "empty text bypasses endpoint",
			text:       "   ",
			response:   `unused`,
			limit:      3,
			want:       []Claim{},
			wantCalled: false,
		},
		{
			name:       "enforces claim limit",
			text:       "Four current facts",
			response:   `{"choices":[{"message":{"content":"{\"claims\":[{\"text\":\"One\",\"domain\":\"other\"},{\"text\":\"Two\",\"domain\":\"company\"},{\"text\":\"Three\",\"domain\":\"schedule\"},{\"text\":\"Four\",\"domain\":\"regulation\"}]}"}}]}`,
			limit:      3,
			want:       []Claim{{Text: "One", Domain: "other"}, {Text: "Two", Domain: "company"}, {Text: "Three", Domain: "schedule"}},
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
					t.Fatalf("Authorization = %q", got)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			extractor := NewJSONExtractor(server.Client(), server.URL, "test-model", "test-key")
			got, err := extractor.Extract(context.Background(), tt.text, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Extract() error = %v, wantErr %v", err, tt.wantErr)
			}
			if called != tt.wantCalled {
				t.Fatalf("endpoint called = %v, want %v", called, tt.wantCalled)
			}
			if !claimsEqual(got, tt.want) {
				t.Fatalf("Extract() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func claimsEqual(got, want []Claim) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] || strings.TrimSpace(got[i].Text) == "" {
			return false
		}
	}
	return true
}
