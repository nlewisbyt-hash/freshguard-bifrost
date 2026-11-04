package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	benchmarkpkg "github.com/phishguard/freshguard-bifrost/internal/benchmark"
	"github.com/phishguard/freshguard-bifrost/internal/freshguard"
)

type recordingVerifier struct {
	claims []freshguard.Claim
}

func (v *recordingVerifier) Verify(_ context.Context, claims []freshguard.Claim) ([]freshguard.ClaimResult, error) {
	v.claims = append(v.claims, claims...)
	status := "verified"
	score := 90
	if claims[0].Text == "stale claim" {
		status = "stale"
		score = 20
	}
	return []freshguard.ClaimResult{{Claim: claims[0], Status: status, Score: score}}, nil
}

func TestRunCasesRoutesCategoriesAndWritesCompleteObservations(t *testing.T) {
	cases := []benchmarkpkg.Case{
		{ID: "current-01", Category: benchmarkpkg.CategoryCurrent, Prompt: "current claim"},
		{ID: "stale-01", Category: benchmarkpkg.CategoryStale, Prompt: "stale claim"},
		{ID: "subjective-01", Category: benchmarkpkg.CategorySubjective, Prompt: "best color"},
		{ID: "unavailable-01", Category: benchmarkpkg.CategoryUnavailable, Prompt: "network case", FailureMode: "timeout"},
	}
	verifier := &recordingVerifier{}
	var factoryCases []benchmarkpkg.Case
	var output bytes.Buffer

	if err := runCases(context.Background(), cases, func(item benchmarkpkg.Case) freshguard.Verifier {
		factoryCases = append(factoryCases, item)
		return verifier
	}, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := len(factoryCases), 2; got != want {
		t.Fatalf("factory cases = %d, want %d", got, want)
	}
	if got, want := len(verifier.claims), 2; got != want {
		t.Fatalf("verifier claims = %d, want %d", got, want)
	}

	decoder := json.NewDecoder(&output)
	observations := make([]benchmarkpkg.Observation, 0, len(cases))
	for decoder.More() {
		var observation benchmarkpkg.Observation
		if err := decoder.Decode(&observation); err != nil {
			t.Fatal(err)
		}
		observations = append(observations, observation)
	}
	if got, want := len(observations), 4; got != want {
		t.Fatalf("observations = %d, want %d", got, want)
	}

	wants := []benchmarkpkg.Observation{
		{ID: "current-01", ActualStatus: benchmarkpkg.StatusVerified, Searched: true, Blocked: false, TavilyQueries: 1},
		{ID: "stale-01", ActualStatus: benchmarkpkg.StatusStale, Searched: true, Blocked: true, TavilyQueries: 1},
		{ID: "subjective-01", ActualStatus: benchmarkpkg.StatusNoClaims, Searched: false, Blocked: false, TavilyQueries: 0},
		{ID: "unavailable-01", ActualStatus: benchmarkpkg.StatusUnavailable, Searched: false, Blocked: false, TavilyQueries: 0},
	}
	for i, want := range wants {
		got := observations[i]
		if got.ID != want.ID || got.ActualStatus != want.ActualStatus || got.Searched != want.Searched || got.Blocked != want.Blocked || got.TavilyQueries != want.TavilyQueries {
			t.Fatalf("observation %d = %#v, want %#v", i, got, want)
		}
		if got.LatencyMS < 0 {
			t.Fatalf("latency = %f", got.LatencyMS)
		}
	}
}

func TestDomainsForCaseExtractsOnlyReferenceHostname(t *testing.T) {
	tests := []struct {
		name string
		item benchmarkpkg.Case
		want string
	}{
		{name: "reference", item: benchmarkpkg.Case{ReferenceURL: "https://www.HackCulture.io/hackathons/event"}, want: "hackculture.io"},
		{name: "missing", item: benchmarkpkg.Case{}, want: ""},
		{name: "invalid", item: benchmarkpkg.Case{ReferenceURL: "://bad"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domainsForCase(tt.item)
			if strings.Join(got, ",") != tt.want {
				t.Fatalf("domainsForCase() = %v, want %q", got, tt.want)
			}
		})
	}
}

func TestURLsForCaseUsesOnlyValidHTTPSReference(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "https://hackculture.io/hackathons/event", want: "https://hackculture.io/hackathons/event"},
		{input: "http://hackculture.io/event", want: ""},
		{input: "://bad", want: ""},
	}
	for _, tt := range tests {
		got := urlsForCase(benchmarkpkg.Case{ReferenceURL: tt.input})
		if strings.Join(got, ",") != tt.want {
			t.Fatalf("urlsForCase(%q) = %v, want %q", tt.input, got, tt.want)
		}
	}
}
