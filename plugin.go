package freshguardbifrost

import (
	"fmt"
	"strings"
	"sync"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/phishguard/freshguard-bifrost/internal/freshguard"
)

type reportContextKey string

const ReportContextKey reportContextKey = "freshguard.verification_report"

const streamBufferContextKey reportContextKey = "freshguard.chat_stream_buffer"

type chatStreamBuffer struct {
	mu      sync.Mutex
	content strings.Builder
}

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
	if bifrostErr != nil {
		clearChatStreamBuffer(ctx)
		return resp, bifrostErr, nil
	}
	if !p.config.Enabled || resp == nil || p.extractor == nil || p.verifier == nil {
		return resp, bifrostErr, nil
	}
	if unsupportedProtectedStream(resp) {
		clearChatStreamBuffer(ctx)
		return nil, unsupportedStreamError(), nil
	}
	content, streaming, final, ok := responseContent(ctx, resp)
	if streaming && !final {
		return nil, skippedStreamChunk(), nil
	}
	if !ok {
		storeReport(ctx, freshguard.VerificationReport{Status: "verification_unavailable"})
		return resp, bifrostErr, nil
	}
	if strings.TrimSpace(*content) == "" {
		if hasStructuredOutput(resp) {
			storeReport(ctx, freshguard.VerificationReport{Status: "no_time_sensitive_claims"})
		}
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

func responseContent(ctx *schemas.BifrostContext, resp *schemas.BifrostResponse) (content *string, streaming, final, ok bool) {
	if streamContent, streamOK := chatStreamContent(resp); streamOK {
		final = isFinalStreamChunk(ctx)
		if ctx == nil {
			return streamContent, true, final, true
		}
		state, _ := ctx.Value(streamBufferContextKey).(*chatStreamBuffer)
		if state == nil {
			state = &chatStreamBuffer{}
			ctx.SetValue(streamBufferContextKey, state)
		}
		state.mu.Lock()
		state.content.WriteString(*streamContent)
		aggregated := state.content.String()
		state.mu.Unlock()
		if final {
			ctx.ClearValue(streamBufferContextKey)
			*streamContent = aggregated
			role := string(schemas.ChatMessageRoleAssistant)
			resp.ChatResponse.Choices[0].ChatStreamResponseChoice.Delta.Role = &role
		}
		return streamContent, true, final, true
	}
	if content, ok = responsesOutputContent(resp); ok {
		return content, false, true, true
	}
	content, ok = nonStreamingContent(resp)
	return content, false, true, ok
}

func unsupportedProtectedStream(resp *schemas.BifrostResponse) bool {
	if resp == nil {
		return false
	}
	if resp.ResponsesStreamResponse != nil {
		return true
	}
	if resp.ChatResponse == nil || len(resp.ChatResponse.Choices) == 0 {
		return false
	}
	choice := resp.ChatResponse.Choices[0].ChatStreamResponseChoice
	return choice != nil && choice.Delta != nil && (choice.Delta.Audio != nil || len(choice.Delta.ToolCalls) > 0)
}

func unsupportedStreamError() *schemas.BifrostError {
	allowFallbacks := false
	status := 422
	errType := "freshguard_unsupported_stream"
	code := "unsupported_protected_stream"
	return &schemas.BifrostError{
		IsBifrostError: true,
		StatusCode:     &status,
		AllowFallbacks: &allowFallbacks,
		Error: &schemas.ErrorField{
			Type: &errType, Code: &code,
			Message: "FreshGuard blocked a stream surface that cannot be verified and replayed losslessly",
		},
	}
}

func responsesOutputContent(resp *schemas.BifrostResponse) (*string, bool) {
	if resp == nil || resp.ResponsesResponse == nil {
		return nil, false
	}
	var targets []*string
	var parts []string
	for i := range resp.ResponsesResponse.Output {
		message := &resp.ResponsesResponse.Output[i]
		if message.Content == nil {
			continue
		}
		if message.Content.ContentStr != nil {
			targets = append(targets, message.Content.ContentStr)
			parts = append(parts, *message.Content.ContentStr)
		}
		for j := range message.Content.ContentBlocks {
			block := &message.Content.ContentBlocks[j]
			if block.Type != schemas.ResponsesOutputMessageContentTypeText || block.Text == nil {
				continue
			}
			targets = append(targets, block.Text)
			parts = append(parts, *block.Text)
		}
	}
	if len(targets) == 0 {
		empty := ""
		return &empty, true
	}
	combined := strings.Join(parts, "")
	*targets[0] = combined
	for _, target := range targets[1:] {
		*target = ""
	}
	return targets[0], true
}

func hasStructuredOutput(resp *schemas.BifrostResponse) bool {
	if resp == nil {
		return false
	}
	if resp.ResponsesResponse != nil && len(resp.ResponsesResponse.Output) > 0 {
		return true
	}
	if resp.ChatResponse == nil || len(resp.ChatResponse.Choices) == 0 {
		return false
	}
	choice := resp.ChatResponse.Choices[0].ChatNonStreamResponseChoice
	return choice != nil && choice.Message != nil && choice.Message.ChatAssistantMessage != nil &&
		(len(choice.Message.ChatAssistantMessage.ToolCalls) > 0 || choice.Message.ChatAssistantMessage.Audio != nil)
}

func chatStreamContent(resp *schemas.BifrostResponse) (*string, bool) {
	if resp == nil || resp.ChatResponse == nil || len(resp.ChatResponse.Choices) == 0 {
		return nil, false
	}
	choice := resp.ChatResponse.Choices[0].ChatStreamResponseChoice
	if choice == nil || choice.Delta == nil {
		return nil, false
	}
	if choice.Delta.Content == nil {
		empty := ""
		choice.Delta.Content = &empty
	}
	return choice.Delta.Content, true
}

func isFinalStreamChunk(ctx *schemas.BifrostContext) bool {
	if ctx == nil {
		return false
	}
	final, _ := ctx.Value(schemas.BifrostContextKeyStreamEndIndicator).(bool)
	return final
}

func skippedStreamChunk() *schemas.BifrostError {
	skip := true
	return &schemas.BifrostError{StreamControl: &schemas.StreamControl{SkipStream: &skip}}
}

func clearChatStreamBuffer(ctx *schemas.BifrostContext) {
	if ctx != nil {
		ctx.ClearValue(streamBufferContextKey)
	}
}

func nonStreamingContent(resp *schemas.BifrostResponse) (*string, bool) {
	if resp.ChatResponse == nil || len(resp.ChatResponse.Choices) == 0 {
		return nil, false
	}
	choice := resp.ChatResponse.Choices[0].ChatNonStreamResponseChoice
	if choice == nil || choice.Message == nil {
		return nil, false
	}
	if choice.Message.Content != nil && choice.Message.Content.ContentStr != nil && strings.TrimSpace(*choice.Message.Content.ContentStr) != "" {
		return choice.Message.Content.ContentStr, true
	}
	if choice.Message.ChatAssistantMessage != nil && choice.Message.ChatAssistantMessage.Audio != nil {
		return &choice.Message.ChatAssistantMessage.Audio.Transcript, true
	}
	if choice.Message.Content == nil || choice.Message.Content.ContentStr == nil {
		empty := ""
		return &empty, true
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
		if result.Status == "stale" || (result.Status == "verified" && result.Score < threshold) {
			report.Status = "stale"
			break
		}
	}
	return report
}

func reportBelowThreshold(report freshguard.VerificationReport, threshold int) bool {
	for _, result := range report.Results {
		if result.Status == "stale" && result.Score < threshold {
			return true
		}
	}
	return false
}

var _ schemas.LLMPlugin = (*Plugin)(nil)
