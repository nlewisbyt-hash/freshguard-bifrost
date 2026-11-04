package freshguard

import (
	"math"
	"time"
)

func Score(claim Claim, evidence []Evidence, now time.Time) ClaimResult {
	result := ClaimResult{Claim: claim, Evidence: evidence, Status: "unverified"}
	if len(evidence) == 0 {
		return result
	}

	var recencyTotal float64
	var recencyCount int
	var entailmentTotal float64
	var supports int
	for _, item := range evidence {
		if !item.PublishedAt.IsZero() {
			recencyTotal += recencyScore(item.PublishedAt, now)
			recencyCount++
		}
		entailmentTotal += clampFloat(item.Entailment, 0, 1) * 100
		if item.Supports {
			supports++
		}
	}

	count := float64(len(evidence))
	result.Components = ScoreComponents{
		SourceAgreement: clampInt(int(math.Round(float64(supports)/count*100)), 0, 100),
		Entailment:      clampInt(int(math.Round(entailmentTotal/count)), 0, 100),
		Coverage:        clampInt(int(math.Round(math.Min(count, 3)/3*100)), 0, 100),
	}
	if recencyCount > 0 {
		result.Components.Recency = clampInt(int(math.Round(recencyTotal/float64(recencyCount))), 0, 100)
	}

	weighted := float64(result.Components.SourceAgreement)*0.30 +
		float64(result.Components.Entailment)*0.25 +
		float64(result.Components.Coverage)*0.10
	weight := 0.65
	if recencyCount > 0 {
		weighted += float64(result.Components.Recency) * 0.35
		weight += 0.35
	}
	weighted /= weight
	result.Score = clampInt(int(math.Round(weighted)), 0, 100)
	if result.Score >= 60 && result.Components.SourceAgreement >= 50 {
		result.Status = "verified"
	} else {
		result.Status = "stale"
	}
	return result
}

func recencyScore(publishedAt, now time.Time) float64 {
	if publishedAt.IsZero() || publishedAt.After(now) {
		return 0
	}
	age := now.Sub(publishedAt)
	switch {
	case age <= 30*24*time.Hour:
		return 100
	case age <= 90*24*time.Hour:
		return 60
	case age <= 365*24*time.Hour:
		return 30
	default:
		return 10
	}
}

func clampFloat(value, low, high float64) float64 {
	return math.Min(math.Max(value, low), high)
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
