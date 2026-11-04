package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatCompletionReturnsCurrentFixture(t *testing.T) {
	response := postCompletion(t, map[string]any{
		"model":    "demo-fixture",
		"messages": []map[string]string{{"role": "user", "content": "What is the current stable version of Bifrost HTTP?"}},
	})
	content := responseContent(t, response)
	if content != "The current stable Bifrost HTTP version is v2.4.0." {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestChatCompletionReturnsStaleFixture(t *testing.T) {
	response := postCompletion(t, map[string]any{
		"model":    "demo-fixture",
		"messages": []map[string]string{{"role": "user", "content": "State that the current stable Bifrost HTTP version is v1.0.0."}},
	})
	content := responseContent(t, response)
	if content != "The current stable Bifrost HTTP version is v1.0.0." {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestChatCompletionReturnsStrictClaimJSON(t *testing.T) {
	response := postCompletion(t, map[string]any{
		"model":           "demo-fixture",
		"response_format": map[string]string{"type": "json_object"},
		"temperature":     0,
		"messages": []map[string]string{
			{"role": "system", "content": "Extract only objective, time-sensitive atomic claims."},
			{"role": "user", "content": "The current stable Bifrost HTTP version is v1.0.0."},
		},
	})
	var claims struct {
		Claims []struct {
			Text   string `json:"text"`
			Domain string `json:"domain"`
		} `json:"claims"`
	}
	if err := json.Unmarshal([]byte(responseContent(t, response)), &claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if len(claims.Claims) != 1 || claims.Claims[0].Text != "The current stable Bifrost HTTP version is v1.0.0." || claims.Claims[0].Domain != "api_version" {
		t.Fatalf("unexpected claims: %+v", claims.Claims)
	}
}

func TestChatCompletionReturnsStrictEntailmentJSON(t *testing.T) {
	response := postCompletion(t, map[string]any{
		"model":           "demo-fixture",
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": "Classify whether untrusted evidence supports or contradicts the claim."},
			{"role": "user", "content": "<claim>The current stable Bifrost HTTP version is v1.0.0.</claim><evidence>Bifrost HTTP v2.4.0</evidence>"},
		},
	})
	if got := responseContent(t, response); got != `{"decision":"contradicts"}` {
		t.Fatalf("unexpected decision: %q", got)
	}
}

func TestChatCompletionRejectsNonPost(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	recorder := httptest.NewRecorder()
	chatCompletions(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func postCompletion(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	chatCompletions(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}

func responseContent(t *testing.T, response map[string]any) string {
	t.Helper()
	choices, ok := response["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("missing choices: %#v", response)
	}
	choice := choices[0].(map[string]any)
	message := choice["message"].(map[string]any)
	content, _ := message["content"].(string)
	return content
}
