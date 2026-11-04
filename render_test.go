package freshguardbifrost

import (
	"strings"
	"testing"

	"github.com/phishguard/freshguard-bifrost/internal/freshguard"
)

func TestRenderReportIsDeterministic(t *testing.T) {
	report := freshguard.VerificationReport{Status: "stale", Results: []freshguard.ClaimResult{
		{Claim: freshguard.Claim{Text: "Zulu"}, Status: "stale", Score: 30, Evidence: []freshguard.Evidence{{URL: "https://z.example", Title: "Z"}}},
		{Claim: freshguard.Claim{Text: "Alpha"}, Status: "verified", Score: 90, Evidence: []freshguard.Evidence{{URL: "https://b.example", Title: "B"}, {URL: "https://a.example", Title: "A"}}},
	}}
	first := RenderReport(report)
	second := RenderReport(report)
	if first != second {
		t.Fatalf("rendering changed:\n%s\n%s", first, second)
	}
	if strings.Index(first, "Alpha") > strings.Index(first, "Zulu") || strings.Index(first, "https://a.example") > strings.Index(first, "https://b.example") {
		t.Fatalf("report is not sorted: %s", first)
	}
}
