package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	benchmarkpkg "github.com/phishguard/freshguard-bifrost/internal/benchmark"
	"github.com/phishguard/freshguard-bifrost/internal/freshguard"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("freshguard-live-benchmark", flag.ContinueOnError)
	flags.SetOutput(stderr)
	casesPath := flags.String("cases", "benchmark/cases.jsonl", "path to the frozen benchmark cases")
	sourceGrounded := flags.Bool("source-grounded", false, "restrict each objective case to its reference URL domain")
	directSource := flags.Bool("direct-source", false, "verify each objective case directly against its reference URL")
	entailmentEndpoint := flags.String("entailment-endpoint", "https://api.openai.com/v1/chat/completions", "OpenAI-compatible entailment endpoint")
	entailmentModel := flags.String("entailment-model", "gpt-4o-mini", "entailment model used with EXTRACTOR_API_KEY")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *sourceGrounded && *directSource {
		fmt.Fprintln(stderr, "-source-grounded and -direct-source are mutually exclusive")
		return 2
	}
	apiKey := strings.TrimSpace(os.Getenv("TAVILY_API_KEY"))
	if apiKey == "" {
		fmt.Fprintln(stderr, "TAVILY_API_KEY is required")
		return 2
	}
	cases, err := benchmarkpkg.LoadCases(*casesPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	client := &http.Client{Timeout: 30 * time.Second}
	var judge freshguard.EntailmentJudge
	if key := strings.TrimSpace(os.Getenv("EXTRACTOR_API_KEY")); *directSource && key != "" {
		judge = freshguard.NewJSONEntailmentJudge(client, *entailmentEndpoint, *entailmentModel, key)
	}
	verifierFor := func(item benchmarkpkg.Case) freshguard.Verifier {
		if *directSource {
			return freshguard.NewDirectURLVerifierWithJudge(client, apiKey, urlsForCase(item), nil, judge)
		}
		if *sourceGrounded {
			return freshguard.NewTavilyVerifierWithDomains(client, apiKey, domainsForCase(item), nil)
		}
		return freshguard.NewTavilyVerifier(client, apiKey, nil)
	}
	if err := runCases(context.Background(), cases, verifierFor, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func urlsForCase(item benchmarkpkg.Case) []string {
	parsed, err := url.Parse(strings.TrimSpace(item.ReferenceURL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil
	}
	parsed.Fragment = ""
	return []string{parsed.String()}
}

func runCases(ctx context.Context, cases []benchmarkpkg.Case, verifierFor func(benchmarkpkg.Case) freshguard.Verifier, output io.Writer) error {
	encoder := json.NewEncoder(output)
	for _, item := range cases {
		started := time.Now()
		observation := benchmarkpkg.Observation{ID: item.ID}

		switch item.Category {
		case benchmarkpkg.CategorySubjective:
			observation.ActualStatus = benchmarkpkg.StatusNoClaims
		case benchmarkpkg.CategoryUnavailable:
			observation.ActualStatus = benchmarkpkg.StatusUnavailable
		default:
			observation.Searched = true
			observation.TavilyQueries = 1
			verifier := verifierFor(item)
			results, err := verifier.Verify(ctx, []freshguard.Claim{{Text: item.Prompt, Domain: "other"}})
			if err != nil {
				observation.ActualStatus = benchmarkpkg.StatusUnavailable
			} else if len(results) == 0 {
				observation.ActualStatus = benchmarkpkg.StatusUnverified
			} else {
				observation.ActualStatus = normalizeStatus(results[0].Status)
				observation.Blocked = observation.ActualStatus == benchmarkpkg.StatusStale
			}
		}

		observation.LatencyMS = float64(time.Since(started).Microseconds()) / 1000
		if err := encoder.Encode(observation); err != nil {
			return fmt.Errorf("encode observation %q: %w", item.ID, err)
		}
	}
	return nil
}

func domainsForCase(item benchmarkpkg.Case) []string {
	parsed, err := url.Parse(strings.TrimSpace(item.ReferenceURL))
	if err != nil || parsed.Hostname() == "" {
		return nil
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	if host == "" {
		return nil
	}
	return []string{host}
}

func normalizeStatus(status string) benchmarkpkg.Status {
	switch benchmarkpkg.Status(status) {
	case benchmarkpkg.StatusVerified, benchmarkpkg.StatusStale, benchmarkpkg.StatusUnverified:
		return benchmarkpkg.Status(status)
	default:
		return benchmarkpkg.StatusUnverified
	}
}
