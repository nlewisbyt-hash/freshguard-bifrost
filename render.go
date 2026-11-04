package freshguardbifrost

import (
	"fmt"
	"sort"
	"strings"

	"github.com/phishguard/freshguard-bifrost/internal/freshguard"
)

func RenderReport(report freshguard.VerificationReport) string {
	var builder strings.Builder
	builder.WriteString("---\nFreshGuard freshness check\n")
	builder.WriteString("Status: ")
	builder.WriteString(strings.ReplaceAll(report.Status, "_", " "))
	if len(report.Results) == 0 {
		return builder.String()
	}

	results := append([]freshguard.ClaimResult(nil), report.Results...)
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Claim.Text < results[j].Claim.Text
	})
	for _, result := range results {
		builder.WriteString("\n\nClaim: ")
		builder.WriteString(result.Claim.Text)
		builder.WriteString(fmt.Sprintf("\nStatus: %s | Score: %d/100", result.Status, result.Score))

		evidence := append([]freshguard.Evidence(nil), result.Evidence...)
		sort.SliceStable(evidence, func(i, j int) bool {
			if evidence[i].URL == evidence[j].URL {
				return evidence[i].Title < evidence[j].Title
			}
			return evidence[i].URL < evidence[j].URL
		})
		for _, item := range evidence {
			builder.WriteString("\n- ")
			if item.Title != "" {
				builder.WriteString(item.Title)
				builder.WriteString(": ")
			}
			builder.WriteString(item.URL)
		}
	}
	return builder.String()
}
