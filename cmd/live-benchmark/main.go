package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
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
	if err := flags.Parse(args); err != nil {
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
	verifier := freshguard.NewTavilyVerifier(&http.Client{Timeout: 30 * time.Second}, apiKey, nil)
	if err := runCases(context.Background(), cases, verifier, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runCases(ctx context.Context, cases []benchmarkpkg.Case, verifier freshguard.Verifier, output io.Writer) error {
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
			results, err := verifier.Verify(ctx, []freshguard.Claim{{Text: item.Prompt, Domain: "other"}})
			if err != nil {
				observation.ActualStatus = benchmarkpkg.StatusUnavailable
			} else if len(results) == 0 {
				observation.ActualStatus = benchmarkpkg.StatusUnverified
			} else {
				observation.ActualStatus = normalizeStatus(results[0].Status)
				observation.Blocked = observation.ActualStatus == benchmarkpkg.StatusStale || observation.ActualStatus == benchmarkpkg.StatusUnverified
			}
		}

		observation.LatencyMS = float64(time.Since(started).Microseconds()) / 1000
		if err := encoder.Encode(observation); err != nil {
			return fmt.Errorf("encode observation %q: %w", item.ID, err)
		}
	}
	return nil
}

func normalizeStatus(status string) benchmarkpkg.Status {
	switch benchmarkpkg.Status(status) {
	case benchmarkpkg.StatusVerified, benchmarkpkg.StatusStale, benchmarkpkg.StatusUnverified:
		return benchmarkpkg.Status(status)
	default:
		return benchmarkpkg.StatusUnverified
	}
}
