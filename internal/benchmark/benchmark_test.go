package benchmark

import (
	"math"
	"path/filepath"
	"testing"
)

func TestLoadCasesRejectsWrongFrozenDistribution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cases.jsonl")
	writeTestFile(t, path, `{"id":"current-01","category":"current","prompt":"current","expected_status":"verified"}
{"id":"stale-01","category":"stale","prompt":"stale","expected_status":"stale"}
`)

	_, err := LoadCases(path)
	if err == nil {
		t.Fatal("LoadCases() error = nil, want frozen-distribution error")
	}
}

func TestLoadCasesAcceptsOfficialFrozenDataset(t *testing.T) {
	path := filepath.Join("..", "..", "benchmark", "cases.jsonl")
	cases, err := LoadCases(path)
	if err != nil {
		t.Fatalf("LoadCases() error = %v", err)
	}
	if got, want := len(cases), 60; got != want {
		t.Fatalf("len(cases) = %d, want %d", got, want)
	}

	wantCounts := map[Category]int{
		CategoryCurrent:     20,
		CategoryStale:       20,
		CategorySubjective:  10,
		CategoryUnavailable: 10,
	}
	gotCounts := map[Category]int{}
	for _, item := range cases {
		gotCounts[item.Category]++
	}
	for category, want := range wantCounts {
		if got := gotCounts[category]; got != want {
			t.Errorf("count[%s] = %d, want %d", category, got, want)
		}
	}
}

func TestEvaluateComputesSubmissionMetrics(t *testing.T) {
	cases := []Case{
		{ID: "current-01", Category: CategoryCurrent, ExpectedStatus: StatusVerified},
		{ID: "current-02", Category: CategoryCurrent, ExpectedStatus: StatusVerified},
		{ID: "stale-01", Category: CategoryStale, ExpectedStatus: StatusStale},
		{ID: "stale-02", Category: CategoryStale, ExpectedStatus: StatusStale},
		{ID: "subjective-01", Category: CategorySubjective, ExpectedStatus: StatusNoClaims},
		{ID: "unavailable-01", Category: CategoryUnavailable, ExpectedStatus: StatusUnavailable},
	}
	observations := []Observation{
		{ID: "current-01", ActualStatus: StatusVerified, Searched: true, LatencyMS: 100, TavilyQueries: 1},
		{ID: "current-02", ActualStatus: StatusStale, Searched: true, Blocked: true, LatencyMS: 300, TavilyQueries: 2},
		{ID: "stale-01", ActualStatus: StatusStale, Searched: true, Blocked: true, LatencyMS: 200, TavilyQueries: 1},
		{ID: "stale-02", ActualStatus: StatusUnverified, Searched: true, LatencyMS: 400, TavilyQueries: 3},
		{ID: "subjective-01", ActualStatus: StatusNoClaims, Searched: false, LatencyMS: 50},
		{ID: "unavailable-01", ActualStatus: StatusUnavailable, Searched: true, LatencyMS: 500, TavilyQueries: 1},
	}

	report, err := Evaluate(cases, observations)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	assertClose(t, "stale recall", report.StaleRecall, 0.5)
	assertClose(t, "current precision", report.CurrentPrecision, 1.0)
	assertClose(t, "subjective bypass", report.SubjectiveBypass, 1.0)
	assertClose(t, "false block rate", report.FalseBlockRate, 1.0/3.0)
	assertClose(t, "average Tavily queries", report.AverageTavilyQueries, 8.0/6.0)
	assertClose(t, "p95 latency", report.P95LatencyMS, 500)

	if got := report.Confusion[StatusStale][StatusUnverified]; got != 1 {
		t.Fatalf("confusion[stale][unverified] = %d, want 1", got)
	}
}

func TestEvaluateRejectsMissingObservation(t *testing.T) {
	cases := []Case{{ID: "current-01", Category: CategoryCurrent, ExpectedStatus: StatusVerified}}
	_, err := Evaluate(cases, nil)
	if err == nil {
		t.Fatal("Evaluate() error = nil, want missing-observation error")
	}
}

func TestEvaluateRejectsDuplicateObservation(t *testing.T) {
	cases := []Case{{ID: "current-01", Category: CategoryCurrent, ExpectedStatus: StatusVerified}}
	observations := []Observation{{ID: "current-01"}, {ID: "current-01"}}
	_, err := Evaluate(cases, observations)
	if err == nil {
		t.Fatal("Evaluate() error = nil, want duplicate-observation error")
	}
}

func TestLoadObservationsParsesJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations.jsonl")
	writeTestFile(t, path, `{"id":"current-01","actual_status":"verified","searched":true,"blocked":false,"latency_ms":123.5,"tavily_queries":2}
{"id":"subjective-01","actual_status":"no_time_sensitive_claims","searched":false,"blocked":false,"latency_ms":20,"tavily_queries":0}
`)

	observations, err := LoadObservations(path)
	if err != nil {
		t.Fatalf("LoadObservations() error = %v", err)
	}
	if got, want := len(observations), 2; got != want {
		t.Fatalf("len(observations) = %d, want %d", got, want)
	}
	if got, want := observations[0].LatencyMS, 123.5; got != want {
		t.Fatalf("LatencyMS = %f, want %f", got, want)
	}
}

func TestLoadObservationsRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations.jsonl")
	writeTestFile(t, path, `{"id":"current-01","actual_status":"verified","unexpected":true}
`)

	_, err := LoadObservations(path)
	if err == nil {
		t.Fatal("LoadObservations() error = nil, want unknown-field error")
	}
}

func assertClose(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %f, want %f", name, got, want)
	}
}
