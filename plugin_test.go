package freshguardbifrost

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/phishguard/freshguard-bifrost/internal/freshguard"
)

type fakeExtractor struct {
	claims []freshguard.Claim
	err    error
	calls  int
}

func (f *fakeExtractor) Extract(_ context.Context, _ string, limit int) ([]freshguard.Claim, error) {
	f.calls++
	claims := append([]freshguard.Claim(nil), f.claims...)
	if len(claims) > limit {
		claims = claims[:limit]
	}
	return claims, f.err
}

type fakeVerifier struct {
	results []freshguard.ClaimResult
	err     error
	calls   int
}

func (f *fakeVerifier) Verify(_ context.Context, _ []freshguard.Claim) ([]freshguard.ClaimResult, error) {
	f.calls++
	return append([]freshguard.ClaimResult(nil), f.results...), f.err
}

func TestPluginPassThroughGuards(t *testing.T) {
	providerErr := &schemas.BifrostError{Error: &schemas.ErrorField{Message: "provider failed"}}
	tests := []struct {
		name string
		resp *schemas.BifrostResponse
		err  *schemas.BifrostError
	}{
		{name: "nil response and error"},
		{name: "provider error", resp: chatResponse("answer"), err: providerErr},
		{name: "unsupported response", resp: &schemas.BifrostResponse{}},
		{name: "empty chat content", resp: chatResponse("  ")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor := &fakeExtractor{claims: []freshguard.Claim{{Text: "fact", Domain: "other"}}}
			verifier := &fakeVerifier{}
			plugin := New(freshguard.Config{Enabled: true, Policy: freshguard.PolicyWarn, MaxClaims: 3}, extractor, verifier)
			gotResp, gotErr, hookErr := plugin.PostLLMHook(schemas.NewBifrostContext(context.Background(), zeroTime()), tt.resp, tt.err)
			if hookErr != nil || gotResp != tt.resp || gotErr != tt.err {
				t.Fatalf("PostLLMHook() = (%p, %p, %v)", gotResp, gotErr, hookErr)
			}
			if extractor.calls != 0 || verifier.calls != 0 {
				t.Fatalf("unexpected calls extractor=%d verifier=%d", extractor.calls, verifier.calls)
			}
		})
	}
}

func TestPluginLimitsClaimsAndStoresReport(t *testing.T) {
	extractor := &fakeExtractor{claims: []freshguard.Claim{{Text: "one"}, {Text: "two"}, {Text: "three"}, {Text: "four"}}}
	verifier := &fakeVerifier{results: []freshguard.ClaimResult{{Claim: freshguard.Claim{Text: "one"}, Status: "verified", Score: 90}}}
	plugin := New(freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow, MaxClaims: 3, Threshold: 60}, extractor, verifier)
	ctx := schemas.NewBifrostContext(context.Background(), zeroTime())
	resp := chatResponse("answer")
	gotResp, gotErr, err := plugin.PostLLMHook(ctx, resp, nil)
	if err != nil || gotErr != nil || gotResp != resp || contentOf(resp) != "answer" {
		t.Fatalf("unexpected result resp=%p err=%v hookErr=%v content=%q", gotResp, gotErr, err, contentOf(resp))
	}
	report, ok := ctx.Value(ReportContextKey).(freshguard.VerificationReport)
	if !ok || len(report.Results) != 1 || report.Status != "verified" {
		t.Fatalf("stored report = %#v, ok=%v", report, ok)
	}
}

func TestPluginWarnAppendsEvidence(t *testing.T) {
	result := staleResult()
	plugin := New(
		freshguard.Config{Enabled: true, Policy: freshguard.PolicyWarn, MaxClaims: 3, Threshold: 60},
		&fakeExtractor{claims: []freshguard.Claim{result.Claim}},
		&fakeVerifier{results: []freshguard.ClaimResult{result}},
	)
	resp := chatResponse("original answer")
	gotResp, gotErr, err := plugin.PostLLMHook(schemas.NewBifrostContext(context.Background(), zeroTime()), resp, nil)
	if err != nil || gotErr != nil || gotResp != resp {
		t.Fatalf("unexpected result: %v %v", gotErr, err)
	}
	content := contentOf(resp)
	for _, want := range []string{"original answer", "FreshGuard freshness check", "Score: 35/100", "https://example.com/old"} {
		if !strings.Contains(content, want) {
			t.Fatalf("content missing %q: %s", want, content)
		}
	}
}

func TestPluginBlockReturnsNonFallbackError(t *testing.T) {
	result := staleResult()
	plugin := New(
		freshguard.Config{Enabled: true, Policy: freshguard.PolicyBlock, MaxClaims: 3, Threshold: 60},
		&fakeExtractor{claims: []freshguard.Claim{result.Claim}},
		&fakeVerifier{results: []freshguard.ClaimResult{result}},
	)
	resp, bifrostErr, err := plugin.PostLLMHook(schemas.NewBifrostContext(context.Background(), zeroTime()), chatResponse("answer"), nil)
	if err != nil || resp != nil || bifrostErr == nil || bifrostErr.AllowFallbacks == nil || *bifrostErr.AllowFallbacks {
		t.Fatalf("unexpected block result resp=%v bifrostErr=%#v err=%v", resp, bifrostErr, err)
	}
	if bifrostErr.Error == nil || !strings.Contains(bifrostErr.Error.Message, "freshness") {
		t.Fatalf("block error = %#v", bifrostErr)
	}
}

func TestPluginBlockDoesNotRejectUnverifiedClaim(t *testing.T) {
	result := freshguard.ClaimResult{
		Claim:  freshguard.Claim{Text: "The event is virtual", Domain: "other"},
		Status: "unverified",
		Score:  0,
	}
	plugin := New(
		freshguard.Config{Enabled: true, Policy: freshguard.PolicyBlock, MaxClaims: 3, Threshold: 60},
		&fakeExtractor{claims: []freshguard.Claim{result.Claim}},
		&fakeVerifier{results: []freshguard.ClaimResult{result}},
	)
	ctx := schemas.NewBifrostContext(context.Background(), zeroTime())
	response := chatResponse("The event is virtual.")

	gotResponse, gotError, err := plugin.PostLLMHook(ctx, response, nil)
	if err != nil || gotError != nil || gotResponse != response {
		t.Fatalf("response = %#v, bifrost error = %#v, err = %v", gotResponse, gotError, err)
	}
	report, ok := ctx.Value(ReportContextKey).(freshguard.VerificationReport)
	if !ok || report.Status != "unverified" {
		t.Fatalf("report = %#v", ctx.Value(ReportContextKey))
	}
}

func TestPluginVerificationUnavailableDoesNotClaimSuccess(t *testing.T) {
	plugin := New(
		freshguard.Config{Enabled: true, Policy: freshguard.PolicyWarn, MaxClaims: 3, Threshold: 60},
		&fakeExtractor{claims: []freshguard.Claim{{Text: "fact"}}},
		&fakeVerifier{err: freshguard.ErrVerificationUnavailable},
	)
	ctx := schemas.NewBifrostContext(context.Background(), zeroTime())
	resp := chatResponse("answer")
	gotResp, gotErr, err := plugin.PostLLMHook(ctx, resp, nil)
	if err != nil || gotErr != nil || gotResp != resp {
		t.Fatalf("unexpected unavailable result: %v %v", gotErr, err)
	}
	report, _ := ctx.Value(ReportContextKey).(freshguard.VerificationReport)
	if report.Status != "verification_unavailable" || strings.Contains(contentOf(resp), "verified") {
		t.Fatalf("report=%#v content=%q", report, contentOf(resp))
	}
}

func TestPluginConcurrentReportsAreIndependent(t *testing.T) {
	plugin := New(
		freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow, MaxClaims: 3, Threshold: 60},
		&concurrentExtractor{},
		&concurrentVerifier{},
	)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := schemas.NewBifrostContext(context.Background(), zeroTime())
			resp := chatResponse("answer")
			_, _, err := plugin.PostLLMHook(ctx, resp, nil)
			if err != nil {
				t.Errorf("request %d: %v", i, err)
			}
			if _, ok := ctx.Value(ReportContextKey).(freshguard.VerificationReport); !ok {
				t.Errorf("request %d missing report", i)
			}
		}(i)
	}
	wg.Wait()
}

type concurrentExtractor struct{}

func (*concurrentExtractor) Extract(_ context.Context, _ string, _ int) ([]freshguard.Claim, error) {
	return []freshguard.Claim{{Text: "fact"}}, nil
}

type concurrentVerifier struct{}

func (*concurrentVerifier) Verify(_ context.Context, claims []freshguard.Claim) ([]freshguard.ClaimResult, error) {
	return []freshguard.ClaimResult{{Claim: claims[0], Status: "verified", Score: 90}}, nil
}

func chatResponse(text string) *schemas.BifrostResponse {
	content := text
	return &schemas.BifrostResponse{ChatResponse: &schemas.BifrostChatResponse{Choices: []schemas.BifrostResponseChoice{{
		ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{Message: &schemas.ChatMessage{
			Role:    schemas.ChatMessageRoleAssistant,
			Content: &schemas.ChatMessageContent{ContentStr: &content},
		}},
	}}}}
}

func contentOf(resp *schemas.BifrostResponse) string {
	return *resp.ChatResponse.Choices[0].ChatNonStreamResponseChoice.Message.Content.ContentStr
}

func staleResult() freshguard.ClaimResult {
	return freshguard.ClaimResult{
		Claim:  freshguard.Claim{Text: "The API is v2", Domain: "api_version"},
		Status: "stale", Score: 35,
		Evidence: []freshguard.Evidence{{URL: "https://example.com/old", Title: "API docs", Snippet: "The API is now v3"}},
	}
}

func zeroTime() time.Time { return time.Time{} }
