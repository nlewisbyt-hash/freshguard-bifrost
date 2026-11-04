package freshguardbifrost

import (
	"fmt"
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/phishguard/freshguard-bifrost/internal/freshguard"
)

type reportContextKey string

const ReportContextKey reportContextKey = "freshguard.verification_report"

type Plugin struct {
	config    freshguard.Config
	extractor freshguard.Extractor
	verifier  freshguard.Verifier
}

func New(config freshguard.Config, extractor freshguard.Extractor, verifier freshguard.Verifier) *Plugin {
	return &Plugin{config: config, extractor: extractor, verifier: verifier}
}

func (p *Plugin) GetName() string { return "freshguard" }

func (p *Plugin) Cleanup() error { return nil }

func (p *Plugin) PreRequestHook(_ *schemas.BifrostContext, _ *schemas.BifrostRequest) error {
	return nil
}

func (p *Plugin) PreLLMHook(_ *schemas.BifrostContext, req *schemas.BifrostRequest) (*schemas.BifrostRequest, *schemas.LLMPluginShortCircuit, error) {
	return req, nil, nil
}

func (p *Plugin) PostLLMHook(ctx *schemas.BifrostContext, resp *schemas.BifrostResponse, bifrostErr *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError, error) {
	if !p.config.Enabled || bifrostErr != nil || resp == nil || p.extractor == nil || p.verifier == nil {
		return resp, bifrostErr, nil
	}
	content, ok := nonStreamingContent(resp)
	if !ok || strings.TrimSpace(*content) == "" {
		return resp, bifrostErr, nil
	}

	limit := p.config.MaxClaims
	if limit <= 0 || limit > 3 {
		limit = 3
	}
	claims, err := p.extractor.Extract(ctx, *content, limit)
	if err != nil {
		report := freshguard.VerificationReport{Status: "verification_unavailable"}
		storeReport(ctx, report)
		if p.config.Policy == freshguard.PolicyWarn {
			*content += "\n\n" + RenderReport(report)
		}
		return resp, nil, nil
	}
	if len(claims) == 0 {
		report := freshguard.VerificationReport{Status: "no_time_sensitive_claims"}
		storeReport(ctx, report)
		return resp, nil, nil
	}
	if len(claims) > limit {
		claims = claims[:limit]
	}

	results, err := p.verifier.Verify(ctx, claims)
	if err != nil {
		report := freshguard.VerificationReport{Status: "verification_unavailable"}
		storeReport(ctx, report)
		if p.config.Policy == freshguard.PolicyWarn {
			*content += "\n\n" + RenderReport(report)
		}
		return resp, nil, nil
	}
	report := buildReport(results, p.config.Threshold)
	storeReport(ctx, report)

	switch p.config.Policy {
	case freshguard.PolicyWarn:
		*content += "\n\n" + RenderReport(report)
	case freshguard.PolicyBlock:
		if reportBelowThreshold(report, p.config.Threshold) {
			allowFallbacks := false
			status := 422
			errType := "freshguard_freshness_policy"
			code := "freshness_check_failed"
			return nil, &schemas.BifrostError{
				IsBifrostError: true,
				StatusCode:     &status,
				AllowFallbacks: &allowFallbacks,
				Error: &schemas.ErrorField{
					Type: &errType, Code: &code,
					Message: fmt.Sprintf("FreshGuard freshness policy blocked the response: one or more claims scored below %d", p.config.Threshold),
				},
			}, nil
		}
	}
	return resp, nil, nil
}

func nonStreamingContent(resp *schemas.BifrostResponse) (*string, bool) {
	if resp.ChatResponse == nil || len(resp.ChatResponse.Choices) == 0 {
		return nil, false
	}
	choice := resp.ChatResponse.Choices[0].ChatNonStreamResponseChoice
	if choice == nil || choice.Message == nil || choice.Message.Content == nil || choice.Message.Content.ContentStr == nil {
		return nil, false
	}
	return choice.Message.Content.ContentStr, true
}

func storeReport(ctx *schemas.BifrostContext, report freshguard.VerificationReport) {
	if ctx != nil {
		ctx.SetValue(ReportContextKey, report)
	}
}

func buildReport(results []freshguard.ClaimResult, threshold int) freshguard.VerificationReport {
	report := freshguard.VerificationReport{Status: "verified", Results: append([]freshguard.ClaimResult(nil), results...)}
	for _, result := range results {
		if result.Status == "unverified" {
			report.Status = "unverified"
		}
		if result.Status == "stale" || result.Score < threshold {
			report.Status = "stale"
			break
		}
	}
	return report
}

func reportBelowThreshold(report freshguard.VerificationReport, threshold int) bool {
	for _, result := range report.Results {
		if result.Score < threshold {
			return true
		}
	}
	return false
}

var _ schemas.LLMPlugin = (*Plugin)(nil)
