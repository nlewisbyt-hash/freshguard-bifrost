package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	benchmarkpkg "github.com/phishguard/freshguard-bifrost/internal/benchmark"
)

func TestRunPrintsCompleteJSONReport(t *testing.T) {
	casesPath := filepath.Join("..", "..", "benchmark", "cases.jsonl")
	cases, err := benchmarkpkg.LoadCases(casesPath)
	if err != nil {
		t.Fatalf("load official cases: %v", err)
	}
	observationsPath := filepath.Join(t.TempDir(), "observations.jsonl")
	file, err := os.Create(observationsPath)
	if err != nil {
		t.Fatalf("create observations: %v", err)
	}
	for index, item := range cases {
		searched := item.Category != benchmarkpkg.CategorySubjective
		queries := 1
		if !searched {
			queries = 0
		}
		observation := benchmarkpkg.Observation{
			ID: item.ID, ActualStatus: item.ExpectedStatus,
			Searched: searched, Blocked: item.Category == benchmarkpkg.CategoryStale,
			LatencyMS: float64(index + 1), TavilyQueries: queries,
		}
		encoded, err := json.Marshal(observation)
		if err != nil {
			t.Fatalf("encode observation: %v", err)
		}
		if _, err := fmt.Fprintln(file, string(encoded)); err != nil {
			t.Fatalf("write observation: %v", err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close observations: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"-cases", casesPath, "-observations", observationsPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	var report benchmarkpkg.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\noutput: %s", err, stdout.String())
	}
	if got, want := report.CaseCount, 60; got != want {
		t.Fatalf("CaseCount = %d, want %d", got, want)
	}
	if got, want := report.StaleRecall, 1.0; got != want {
		t.Fatalf("StaleRecall = %f, want %f", got, want)
	}
}
