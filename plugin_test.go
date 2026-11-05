package freshguardbifrost

import (
	"context"
	"fmt"
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

type recordingExtractor struct {
	mu    sync.Mutex
	texts []string
}

func (r *recordingExtractor) Extract(_ context.Context, text string, _ int) ([]freshguard.Claim, error) {
	r.mu.Lock()
	r.texts = append(r.texts, text)
	r.mu.Unlock()
	return []freshguard.Claim{{Text: text, Domain: "other"}}, nil
}

func (r *recordingExtractor) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.texts...)
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

func TestPluginVerifiesResponsesAPIOutputText(t *testing.T) {
	extractor := &recordingExtractor{}
	verifier := &fakeVerifier{results: []freshguard.ClaimResult{{Claim: freshguard.Claim{Text: "fact"}, Status: "verified", Score: 90}}}
	plugin := New(freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow, MaxClaims: 3, Threshold: 70}, extractor, verifier)
	text := "The current API version is 2.4.0."
	typeMessage := schemas.ResponsesMessageTypeMessage
	role := schemas.ResponsesInputMessageRoleAssistant
	response := &schemas.BifrostResponse{ResponsesResponse: &schemas.BifrostResponsesResponse{Output: []schemas.ResponsesMessage{{
		Type: &typeMessage, Role: &role, Content: &schemas.ResponsesMessageContent{ContentBlocks: []schemas.ResponsesMessageContentBlock{{
			Type: schemas.ResponsesOutputMessageContentTypeText, Text: &text, ResponsesOutputMessageContentText: &schemas.ResponsesOutputMessageContentText{},
		}}},
	}}}}

	got, gotErr, err := plugin.PostLLMHook(schemas.NewBifrostContext(context.Background(), zeroTime()), response, nil)
	if err != nil || gotErr != nil || got != response {
		t.Fatalf("PostLLMHook() = (%p, %p, %v)", got, gotErr, err)
	}
	if texts := extractor.snapshot(); len(texts) != 1 || texts[0] != text {
		t.Fatalf("extractor texts = %#v, want %q", texts, text)
	}
}

func TestPluginVerifiesChatAudioTranscript(t *testing.T) {
	extractor := &recordingExtractor{}
	verifier := &fakeVerifier{results: []freshguard.ClaimResult{{Status: "verified", Score: 90}}}
	plugin := New(freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow, MaxClaims: 3}, extractor, verifier)
	transcript := "The price is 20 dollars."
	response := chatResponse("")
	response.ChatResponse.Choices[0].ChatNonStreamResponseChoice.Message.ChatAssistantMessage = &schemas.ChatAssistantMessage{
		Audio: &schemas.ChatAudioMessageAudio{Transcript: transcript},
	}

	_, gotErr, err := plugin.PostLLMHook(schemas.NewBifrostContext(context.Background(), zeroTime()), response, nil)
	if err != nil || gotErr != nil {
		t.Fatalf("PostLLMHook() error = (%p, %v)", gotErr, err)
	}
	if texts := extractor.snapshot(); len(texts) != 1 || texts[0] != transcript {
		t.Fatalf("extractor texts = %#v, want %q", texts, transcript)
	}
}

func TestPluginRecordsToolOnlyResponsesAsNoVisibleClaims(t *testing.T) {
	plugin := New(freshguard.Config{Enabled: true, Policy: freshguard.PolicyBlock, MaxClaims: 3}, &fakeExtractor{}, &fakeVerifier{})
	ctx := schemas.NewBifrostContext(context.Background(), zeroTime())
	typeCall := schemas.ResponsesMessageTypeFunctionCall
	response := &schemas.BifrostResponse{ResponsesResponse: &schemas.BifrostResponsesResponse{Output: []schemas.ResponsesMessage{{Type: &typeCall}}}}

	got, gotErr, err := plugin.PostLLMHook(ctx, response, nil)
	if err != nil || gotErr != nil || got != response {
		t.Fatalf("PostLLMHook() = (%p, %p, %v)", got, gotErr, err)
	}
	report, ok := ctx.Value(ReportContextKey).(freshguard.VerificationReport)
	if !ok || report.Status != "no_time_sensitive_claims" {
		t.Fatalf("report = %#v, want no_time_sensitive_claims", report)
	}
}

func TestPluginBlocksUnsupportedResponsesStreamBeforeRelease(t *testing.T) {
	plugin := New(freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow}, &fakeExtractor{}, &fakeVerifier{})
	delta := "must not leak"
	response := &schemas.BifrostResponse{ResponsesStreamResponse: &schemas.BifrostResponsesStreamResponse{
		Type: schemas.ResponsesStreamResponseTypeOutputTextDelta, Delta: &delta,
	}}

	got, gotErr, err := plugin.PostLLMHook(schemas.NewBifrostContext(context.Background(), zeroTime()), response, nil)
	if err != nil || got != nil || gotErr == nil || gotErr.StreamControl != nil {
		t.Fatalf("PostLLMHook() = (%p, %#v, %v), want explicit protection error", got, gotErr, err)
	}
}

func TestPluginBlocksUnsupportedChatAudioAndToolStreams(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*schemas.ChatStreamResponseChoiceDelta)
	}{
		{name: "audio", mutate: func(delta *schemas.ChatStreamResponseChoiceDelta) {
			delta.Audio = &schemas.ChatAudioMessageAudio{Transcript: "spoken claim"}
		}},
		{name: "tool call", mutate: func(delta *schemas.ChatStreamResponseChoiceDelta) {
			delta.ToolCalls = []schemas.ChatAssistantMessageToolCall{{}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := New(freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow}, &fakeExtractor{}, &fakeVerifier{})
			response := chatStreamResponse("must not leak", false)
			tt.mutate(response.ChatResponse.Choices[0].ChatStreamResponseChoice.Delta)
			got, gotErr, err := plugin.PostLLMHook(schemas.NewBifrostContext(context.Background(), zeroTime()), response, nil)
			if err != nil || got != nil || gotErr == nil || gotErr.StreamControl != nil {
				t.Fatalf("PostLLMHook() = (%p, %#v, %v), want explicit protection error", got, gotErr, err)
			}
		})
	}
}

func TestPluginMixedChatTextAndToolsPreservesTools(t *testing.T) {
	extractor := &recordingExtractor{}
	plugin := New(freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow, MaxClaims: 3}, extractor, &fakeVerifier{results: []freshguard.ClaimResult{{Status: "verified", Score: 90}}})
	response := chatResponse("visible claim")
	tools := []schemas.ChatAssistantMessageToolCall{{}}
	response.ChatResponse.Choices[0].ChatNonStreamResponseChoice.Message.ChatAssistantMessage = &schemas.ChatAssistantMessage{ToolCalls: tools}

	got, gotErr, err := plugin.PostLLMHook(schemas.NewBifrostContext(context.Background(), zeroTime()), response, nil)
	if err != nil || gotErr != nil || got != response {
		t.Fatalf("PostLLMHook() = (%p, %#v, %v)", got, gotErr, err)
	}
	if len(response.ChatResponse.Choices[0].ChatNonStreamResponseChoice.Message.ChatAssistantMessage.ToolCalls) != len(tools) {
		t.Fatal("tool calls were modified")
	}
	if texts := extractor.snapshot(); len(texts) != 1 || texts[0] != "visible claim" {
		t.Fatalf("extractor texts = %#v", texts)
	}
}

func TestPluginMalformedResponseRecordsVerificationUnavailable(t *testing.T) {
	plugin := New(freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow}, &fakeExtractor{}, &fakeVerifier{})
	ctx := schemas.NewBifrostContext(context.Background(), zeroTime())
	response := &schemas.BifrostResponse{}

	got, gotErr, err := plugin.PostLLMHook(ctx, response, nil)
	if err != nil || gotErr != nil || got != response {
		t.Fatalf("PostLLMHook() = (%p, %#v, %v)", got, gotErr, err)
	}
	report, ok := ctx.Value(ReportContextKey).(freshguard.VerificationReport)
	if !ok || report.Status != "verification_unavailable" {
		t.Fatalf("report = %#v, want verification_unavailable", report)
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

func TestPluginChatStreamBuffersUntilFinalChunk(t *testing.T) {
	extractor := &recordingExtractor{}
	plugin := New(
		freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow, MaxClaims: 3, Threshold: 60},
		extractor,
		&concurrentVerifier{},
	)
	ctx := schemas.NewBifrostContext(context.Background(), zeroTime())

	for _, part := range []string{"The API ", "is v3."} {
		ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, false)
		gotResponse, gotError, err := plugin.PostLLMHook(ctx, chatStreamResponse(part, false), nil)
		if err != nil || gotResponse != nil || !isSkippedStreamChunk(gotError) {
			t.Fatalf("non-final chunk = (%#v, %#v, %v), want suppressed", gotResponse, gotError, err)
		}
	}

	ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
	gotResponse, gotError, err := plugin.PostLLMHook(ctx, chatStreamResponse("", true), nil)
	if err != nil || gotError != nil || gotResponse == nil {
		t.Fatalf("final chunk = (%#v, %#v, %v)", gotResponse, gotError, err)
	}
	if got := streamContentOf(gotResponse); got != "The API is v3." {
		t.Fatalf("final content = %q", got)
	}
	if got := extractor.snapshot(); len(got) != 1 || got[0] != "The API is v3." {
		t.Fatalf("extractor texts = %#v", got)
	}
}

func TestPluginChatStreamWarnAppendsReportOnlyAfterFinalChunk(t *testing.T) {
	result := staleResult()
	plugin := New(
		freshguard.Config{Enabled: true, Policy: freshguard.PolicyWarn, MaxClaims: 3, Threshold: 60},
		&fakeExtractor{claims: []freshguard.Claim{result.Claim}},
		&fakeVerifier{results: []freshguard.ClaimResult{result}},
	)
	ctx := schemas.NewBifrostContext(context.Background(), zeroTime())
	ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, false)
	response, bifrostErr, err := plugin.PostLLMHook(ctx, chatStreamResponse("stale answer", false), nil)
	if err != nil || response != nil || !isSkippedStreamChunk(bifrostErr) {
		t.Fatalf("non-final = (%#v, %#v, %v)", response, bifrostErr, err)
	}

	ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
	response, bifrostErr, err = plugin.PostLLMHook(ctx, chatStreamResponse("", true), nil)
	if err != nil || bifrostErr != nil || response == nil {
		t.Fatalf("final = (%#v, %#v, %v)", response, bifrostErr, err)
	}
	content := streamContentOf(response)
	if !strings.Contains(content, "stale answer") || !strings.Contains(content, "FreshGuard freshness check") {
		t.Fatalf("final content = %q", content)
	}
}

func TestPluginChatStreamBlockLeaksNoAssistantText(t *testing.T) {
	result := staleResult()
	plugin := New(
		freshguard.Config{Enabled: true, Policy: freshguard.PolicyBlock, MaxClaims: 3, Threshold: 60},
		&fakeExtractor{claims: []freshguard.Claim{result.Claim}},
		&fakeVerifier{results: []freshguard.ClaimResult{result}},
	)
	ctx := schemas.NewBifrostContext(context.Background(), zeroTime())
	ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, false)
	response, bifrostErr, err := plugin.PostLLMHook(ctx, chatStreamResponse("must remain private", false), nil)
	if err != nil || response != nil || !isSkippedStreamChunk(bifrostErr) {
		t.Fatalf("non-final = (%#v, %#v, %v)", response, bifrostErr, err)
	}

	ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
	response, bifrostErr, err = plugin.PostLLMHook(ctx, chatStreamResponse("", true), nil)
	if err != nil || response != nil || bifrostErr == nil || bifrostErr.StreamControl != nil {
		t.Fatalf("final block = (%#v, %#v, %v)", response, bifrostErr, err)
	}
	if bifrostErr.Error == nil || !strings.Contains(bifrostErr.Error.Message, "freshness") {
		t.Fatalf("block error = %#v", bifrostErr)
	}
}

func TestPluginChatStreamProviderErrorDiscardsBufferedText(t *testing.T) {
	extractor := &recordingExtractor{}
	plugin := New(
		freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow, MaxClaims: 3, Threshold: 60},
		extractor,
		&concurrentVerifier{},
	)
	ctx := schemas.NewBifrostContext(context.Background(), zeroTime())
	ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, false)
	_, _, _ = plugin.PostLLMHook(ctx, chatStreamResponse("abandoned", false), nil)
	providerErr := &schemas.BifrostError{Error: &schemas.ErrorField{Message: "provider failed"}}
	response, gotProviderErr, err := plugin.PostLLMHook(ctx, nil, providerErr)
	if err != nil || response != nil || gotProviderErr != providerErr {
		t.Fatalf("provider error = (%#v, %#v, %v)", response, gotProviderErr, err)
	}

	ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
	response, gotProviderErr, err = plugin.PostLLMHook(ctx, chatStreamResponse("fresh", true), nil)
	if err != nil || gotProviderErr != nil || response == nil || streamContentOf(response) != "fresh" {
		t.Fatalf("new final = (%#v, %#v, %v), content %q", response, gotProviderErr, err, streamContentOf(response))
	}
	if got := extractor.snapshot(); len(got) != 1 || got[0] != "fresh" {
		t.Fatalf("extractor texts = %#v", got)
	}
}

func TestPluginChatStreamConcurrentContextsRemainIndependent(t *testing.T) {
	extractor := &recordingExtractor{}
	plugin := New(
		freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow, MaxClaims: 3, Threshold: 60},
		extractor,
		&concurrentVerifier{},
	)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := schemas.NewBifrostContext(context.Background(), zeroTime())
			ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, false)
			_, _, _ = plugin.PostLLMHook(ctx, chatStreamResponse("prefix-", false), nil)
			ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
			want := fmt.Sprintf("prefix-%d", i)
			response, bifrostErr, err := plugin.PostLLMHook(ctx, chatStreamResponse(fmt.Sprint(i), true), nil)
			if err != nil || bifrostErr != nil || response == nil || streamContentOf(response) != want {
				t.Errorf("stream %d = (%#v, %#v, %v), content %q", i, response, bifrostErr, err, streamContentOf(response))
			}
		}(i)
	}
	wg.Wait()
}

func TestPluginChatStreamReassemblesEverySplitPoint(t *testing.T) {
	const sentence = "价格更新：API v3 costs 20 元 per million tokens."
	runes := []rune(sentence)
	for split := 0; split <= len(runes); split++ {
		t.Run(fmt.Sprintf("split-%d", split), func(t *testing.T) {
			extractor := &recordingExtractor{}
			plugin := New(
				freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow, MaxClaims: 3, Threshold: 60},
				extractor,
				&concurrentVerifier{},
			)
			ctx := schemas.NewBifrostContext(context.Background(), zeroTime())
			ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, false)
			_, firstErr, err := plugin.PostLLMHook(ctx, chatStreamResponse(string(runes[:split]), false), nil)
			if err != nil || !isSkippedStreamChunk(firstErr) {
				t.Fatalf("first chunk = (%#v, %v)", firstErr, err)
			}
			ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
			response, finalErr, err := plugin.PostLLMHook(ctx, chatStreamResponse(string(runes[split:]), true), nil)
			if err != nil || finalErr != nil || streamContentOf(response) != sentence {
				t.Fatalf("reassembled = %q, err=%#v hook=%v", streamContentOf(response), finalErr, err)
			}
		})
	}
}

func FuzzPluginChatStreamTwoChunks(f *testing.F) {
	f.Add("API v3", uint(3))
	f.Add("价格 20 元", uint(4))
	f.Add("", uint(0))
	f.Fuzz(func(t *testing.T, text string, rawSplit uint) {
		runes := []rune(text)
		split := 0
		if len(runes) > 0 {
			split = int(rawSplit % uint(len(runes)+1))
		}
		plugin := New(
			freshguard.Config{Enabled: true, Policy: freshguard.PolicyAllow, MaxClaims: 3, Threshold: 60},
			&recordingExtractor{},
			&concurrentVerifier{},
		)
		ctx := schemas.NewBifrostContext(context.Background(), zeroTime())
		ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, false)
		_, _, _ = plugin.PostLLMHook(ctx, chatStreamResponse(string(runes[:split]), false), nil)
		ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
		response, bifrostErr, err := plugin.PostLLMHook(ctx, chatStreamResponse(string(runes[split:]), true), nil)
		if err != nil || bifrostErr != nil || streamContentOf(response) != text {
			t.Fatalf("reassembled = %q, want %q, err=%#v hook=%v", streamContentOf(response), text, bifrostErr, err)
		}
	})
}

func FuzzResponsesOutputContentPreservesVisibleText(f *testing.F) {
	f.Add("price ", "20元")
	f.Add("🙂", " café")
	f.Add("", "tail")
	f.Fuzz(func(t *testing.T, first, second string) {
		want := first + second
		typeMessage := schemas.ResponsesMessageTypeMessage
		role := schemas.ResponsesInputMessageRoleAssistant
		response := &schemas.BifrostResponse{ResponsesResponse: &schemas.BifrostResponsesResponse{Output: []schemas.ResponsesMessage{{
			Type: &typeMessage, Role: &role, Content: &schemas.ResponsesMessageContent{ContentBlocks: []schemas.ResponsesMessageContentBlock{
				{Type: schemas.ResponsesOutputMessageContentTypeText, Text: &first, ResponsesOutputMessageContentText: &schemas.ResponsesOutputMessageContentText{}},
				{Type: schemas.ResponsesOutputMessageContentTypeText, Text: &second, ResponsesOutputMessageContentText: &schemas.ResponsesOutputMessageContentText{}},
			}},
		}}}}

		content, ok := responsesOutputContent(response)
		if !ok || content == nil || *content != want {
			t.Fatalf("responsesOutputContent() = (%v, %v), want %q", content, ok, want)
		}
		blocks := response.ResponsesResponse.Output[0].Content.ContentBlocks
		if *blocks[0].Text+*blocks[1].Text != want {
			t.Fatalf("rendered text changed: %q + %q", *blocks[0].Text, *blocks[1].Text)
		}
	})
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

func chatStreamResponse(text string, final bool) *schemas.BifrostResponse {
	content := text
	choice := schemas.BifrostResponseChoice{
		ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
			Delta: &schemas.ChatStreamResponseChoiceDelta{Content: &content},
		},
	}
	if final {
		finishReason := "stop"
		choice.FinishReason = &finishReason
	}
	return &schemas.BifrostResponse{ChatResponse: &schemas.BifrostChatResponse{Choices: []schemas.BifrostResponseChoice{choice}}}
}

func streamContentOf(resp *schemas.BifrostResponse) string {
	if resp == nil || resp.ChatResponse == nil || len(resp.ChatResponse.Choices) == 0 ||
		resp.ChatResponse.Choices[0].ChatStreamResponseChoice == nil ||
		resp.ChatResponse.Choices[0].ChatStreamResponseChoice.Delta == nil ||
		resp.ChatResponse.Choices[0].ChatStreamResponseChoice.Delta.Content == nil {
		return ""
	}
	return *resp.ChatResponse.Choices[0].ChatStreamResponseChoice.Delta.Content
}

func isSkippedStreamChunk(err *schemas.BifrostError) bool {
	return err != nil && err.StreamControl != nil && err.StreamControl.SkipStream != nil && *err.StreamControl.SkipStream
}

func staleResult() freshguard.ClaimResult {
	return freshguard.ClaimResult{
		Claim:  freshguard.Claim{Text: "The API is v2", Domain: "api_version"},
		Status: "stale", Score: 35,
		Evidence: []freshguard.Evidence{{URL: "https://example.com/old", Title: "API docs", Snippet: "The API is now v3"}},
	}
}

func zeroTime() time.Time { return time.Time{} }
