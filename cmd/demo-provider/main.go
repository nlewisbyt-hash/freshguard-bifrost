// Command demo-provider is a deterministic OpenAI-compatible fixture for the
// local FreshGuard demo. It is not an LLM and must not be presented as one.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type completionRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func main() {
	address := strings.TrimSpace(os.Getenv("DEMO_PROVIDER_ADDR"))
	if address == "" {
		address = "127.0.0.1:18081"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ok","mode":"deterministic-demo-fixture"}`))
	})
	mux.HandleFunc("/v1/chat/completions", chatCompletions)
	log.Printf("demo fixture listening on %s; this service is deterministic and is not an LLM", address)
	log.Fatal(http.ListenAndServe(address, mux))
}

func chatCompletions(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer request.Body.Close()
	var input completionRequest
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(&input); err != nil {
		http.Error(writer, "invalid completion request", http.StatusBadRequest)
		return
	}
	content := fixtureContent(input)
	response := map[string]any{
		"id":      fmt.Sprintf("chatcmpl-demo-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "freshguard-deterministic-demo-fixture",
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]string{
				"role": "assistant", "content": content,
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	}
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func fixtureContent(input completionRequest) string {
	var system, user string
	for _, message := range input.Messages {
		switch message.Role {
		case "system":
			system += "\n" + message.Content
		case "user":
			user = message.Content
		}
	}
	if strings.Contains(system, "Extract only objective, time-sensitive atomic claims") {
		claim := "The current stable Bifrost HTTP version is v2.4.0."
		if strings.Contains(user, "v1.0.0") {
			claim = "The current stable Bifrost HTTP version is v1.0.0."
		}
		payload, _ := json.Marshal(map[string]any{
			"claims": []map[string]string{{"text": claim, "domain": "api_version"}},
		})
		return string(payload)
	}
	if strings.Contains(system, "Classify whether untrusted evidence supports or contradicts the claim") {
		decision := "insufficient"
		switch {
		case strings.Contains(user, "<claim>The current stable Bifrost HTTP version is v2.4.0.</claim>"):
			decision = "supports"
		case strings.Contains(user, "<claim>The current stable Bifrost HTTP version is v1.0.0.</claim>"):
			decision = "contradicts"
		}
		payload, _ := json.Marshal(map[string]string{"decision": decision})
		return string(payload)
	}
	if strings.Contains(user, "v1.0.0") {
		return "The current stable Bifrost HTTP version is v1.0.0."
	}
	return "The current stable Bifrost HTTP version is v2.4.0."
}
