package freshguard

import (
	"testing"
	"time"
)

func TestScoreRecentAgreeingEvidenceBeatsStaleConflictingEvidence(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	claim := Claim{Text: "The API costs $2 per million tokens", Domain: "pricing"}

	recent := []Evidence{
		{URL: "https://vendor.example/pricing", PublishedAt: now.Add(-24 * time.Hour), Supports: true, Entailment: 0.95},
		{URL: "https://docs.example/pricing", PublishedAt: now.Add(-72 * time.Hour), Supports: true, Entailment: 0.90},
		{URL: "https://news.example/update", PublishedAt: now.Add(-7 * 24 * time.Hour), Supports: true, Entailment: 0.85},
	}
	stale := []Evidence{
		{URL: "https://old.example/one", PublishedAt: now.Add(-400 * 24 * time.Hour), Supports: true, Entailment: 0.40},
		{URL: "https://old.example/two", PublishedAt: now.Add(-500 * 24 * time.Hour), Supports: false, Entailment: 0.30},
	}

	recentResult := Score(claim, recent, now)
	staleResult := Score(claim, stale, now)

	if recentResult.Score <= staleResult.Score {
		t.Fatalf("recent score %d must exceed stale score %d", recentResult.Score, staleResult.Score)
	}
	if recentResult.Status != "verified" {
		t.Fatalf("recent status = %q, want verified", recentResult.Status)
	}
	if staleResult.Status != "stale" {
		t.Fatalf("stale status = %q, want stale", staleResult.Status)
	}
}

func TestScoreNoEvidenceIsUnverified(t *testing.T) {
	result := Score(Claim{Text: "Current fact", Domain: "company"}, nil, time.Now())

	if result.Status != "unverified" {
		t.Fatalf("Status = %q, want unverified", result.Status)
	}
	if result.Score != 0 {
		t.Fatalf("Score = %d, want 0", result.Score)
	}
	if result.Components != (ScoreComponents{}) {
		t.Fatalf("Components = %#v, want zero values", result.Components)
	}
}

func TestScoreDoesNotTreatMissingPublicationDateAsStale(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	result := Score(
		Claim{Text: "The current documentation confirms this claim", Domain: "other"},
		[]Evidence{
			{URL: "https://docs.example/current", Supports: true, Entailment: 0.8},
			{URL: "https://vendor.example/current", Supports: true, Entailment: 0.7},
			{URL: "https://news.example/current", Supports: true, Entailment: 0.6},
		},
		now,
	)

	if result.Status != "verified" {
		t.Fatalf("Status = %q, want verified (score=%d, components=%#v)", result.Status, result.Score, result.Components)
	}
	if result.Components.Recency != 0 {
		t.Fatalf("Recency = %d, want 0 to expose unavailable metadata", result.Components.Recency)
	}
}

func TestScoreExposesComponentScores(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	result := Score(
		Claim{Text: "Version 3 is current", Domain: "api_version"},
		[]Evidence{{URL: "https://docs.example/version", PublishedAt: now.Add(-48 * time.Hour), Supports: true, Entailment: 0.8}},
		now,
	)

	components := result.Components
	if components.Recency == 0 || components.SourceAgreement == 0 || components.Entailment == 0 || components.Coverage == 0 {
		t.Fatalf("component scores must be visible and non-zero: %#v", components)
	}
	for name, value := range map[string]int{
		"recency":    components.Recency,
		"agreement":  components.SourceAgreement,
		"entailment": components.Entailment,
		"coverage":   components.Coverage,
	} {
		if value < 0 || value > 100 {
			t.Fatalf("%s component = %d, want 0..100", name, value)
		}
	}
}
