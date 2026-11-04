package benchmark

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

type Category string

const (
	CategoryCurrent     Category = "current"
	CategoryStale       Category = "stale"
	CategorySubjective  Category = "subjective"
	CategoryUnavailable Category = "unavailable"
)

type Status string

const (
	StatusVerified    Status = "verified"
	StatusStale       Status = "stale"
	StatusUnverified  Status = "unverified"
	StatusUnavailable Status = "verification_unavailable"
	StatusNoClaims    Status = "no_time_sensitive_claims"
)

type Case struct {
	ID             string   `json:"id"`
	Category       Category `json:"category"`
	Prompt         string   `json:"prompt"`
	ExpectedStatus Status   `json:"expected_status"`
	ReferenceURL   string   `json:"reference_url,omitempty"`
	FailureMode    string   `json:"failure_mode,omitempty"`
}

type Observation struct {
	ID            string  `json:"id"`
	ActualStatus  Status  `json:"actual_status"`
	Searched      bool    `json:"searched"`
	Blocked       bool    `json:"blocked"`
	LatencyMS     float64 `json:"latency_ms"`
	TavilyQueries int     `json:"tavily_queries"`
}

type Report struct {
	CaseCount            int                       `json:"case_count"`
	Confusion            map[Status]map[Status]int `json:"confusion"`
	StaleRecall          float64                   `json:"stale_recall"`
	CurrentPrecision     float64                   `json:"current_precision"`
	SubjectiveBypass     float64                   `json:"subjective_bypass"`
	FalseBlockRate       float64                   `json:"false_block_rate"`
	P95LatencyMS         float64                   `json:"p95_latency_ms"`
	AverageTavilyQueries float64                   `json:"average_tavily_queries"`
}

func LoadCases(path string) ([]Case, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open benchmark cases: %w", err)
	}
	defer file.Close()

	cases := make([]Case, 0, 60)
	seen := map[string]struct{}{}
	counts := map[Category]int{}
	lineNumber := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item Case
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&item); err != nil {
			return nil, fmt.Errorf("decode benchmark case line %d: %w", lineNumber, err)
		}
		if err := validateCase(item); err != nil {
			return nil, fmt.Errorf("benchmark case line %d: %w", lineNumber, err)
		}
		if _, exists := seen[item.ID]; exists {
			return nil, fmt.Errorf("benchmark case line %d: duplicate id %q", lineNumber, item.ID)
		}
		seen[item.ID] = struct{}{}
		counts[item.Category]++
		cases = append(cases, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read benchmark cases: %w", err)
	}

	wantCounts := map[Category]int{
		CategoryCurrent: 20, CategoryStale: 20,
		CategorySubjective: 10, CategoryUnavailable: 10,
	}
	if len(cases) != 60 {
		return nil, fmt.Errorf("frozen benchmark requires 60 cases, got %d", len(cases))
	}
	for category, want := range wantCounts {
		if got := counts[category]; got != want {
			return nil, fmt.Errorf("frozen benchmark requires %d %s cases, got %d", want, category, got)
		}
	}
	return cases, nil
}

func LoadObservations(path string) ([]Observation, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open benchmark observations: %w", err)
	}
	defer file.Close()

	observations := []Observation{}
	lineNumber := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var observation Observation
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&observation); err != nil {
			return nil, fmt.Errorf("decode benchmark observation line %d: %w", lineNumber, err)
		}
		if strings.TrimSpace(observation.ID) == "" || strings.TrimSpace(string(observation.ActualStatus)) == "" {
			return nil, fmt.Errorf("benchmark observation line %d: id and actual_status are required", lineNumber)
		}
		observations = append(observations, observation)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read benchmark observations: %w", err)
	}
	return observations, nil
}

func validateCase(item Case) error {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" || strings.TrimSpace(item.Prompt) == "" {
		return fmt.Errorf("id and prompt are required")
	}
	wantStatus := map[Category]Status{
		CategoryCurrent: StatusVerified, CategoryStale: StatusStale,
		CategorySubjective: StatusNoClaims, CategoryUnavailable: StatusUnavailable,
	}
	want, ok := wantStatus[item.Category]
	if !ok {
		return fmt.Errorf("unsupported category %q", item.Category)
	}
	if item.ExpectedStatus != want {
		return fmt.Errorf("category %q requires expected_status %q", item.Category, want)
	}
	if (item.Category == CategoryCurrent || item.Category == CategoryStale) && strings.TrimSpace(item.ReferenceURL) == "" {
		return fmt.Errorf("category %q requires reference_url", item.Category)
	}
	if item.Category == CategoryUnavailable && strings.TrimSpace(item.FailureMode) == "" {
		return fmt.Errorf("unavailable case requires failure_mode")
	}
	return nil
}

func Evaluate(cases []Case, observations []Observation) (Report, error) {
	report := Report{CaseCount: len(cases), Confusion: map[Status]map[Status]int{}}
	caseByID := make(map[string]Case, len(cases))
	for _, item := range cases {
		if _, exists := caseByID[item.ID]; exists {
			return Report{}, fmt.Errorf("duplicate case id %q", item.ID)
		}
		caseByID[item.ID] = item
	}

	observationByID := make(map[string]Observation, len(observations))
	for _, observation := range observations {
		if _, exists := observationByID[observation.ID]; exists {
			return Report{}, fmt.Errorf("duplicate observation id %q", observation.ID)
		}
		if _, exists := caseByID[observation.ID]; !exists {
			return Report{}, fmt.Errorf("observation references unknown case %q", observation.ID)
		}
		if observation.LatencyMS < 0 || observation.TavilyQueries < 0 || observation.TavilyQueries > 3 {
			return Report{}, fmt.Errorf("observation %q has invalid measurements", observation.ID)
		}
		observationByID[observation.ID] = observation
	}
	if len(observationByID) != len(caseByID) {
		return Report{}, fmt.Errorf("observations are incomplete: got %d, want %d", len(observationByID), len(caseByID))
	}

	var staleTotal, staleCorrect int
	var verifiedPredictions, verifiedCurrent int
	var subjectiveTotal, subjectiveBypassed int
	var validTotal, falseBlocks int
	latencies := make([]float64, 0, len(cases))
	queryTotal := 0
	for _, item := range cases {
		observation, exists := observationByID[item.ID]
		if !exists {
			return Report{}, fmt.Errorf("missing observation for case %q", item.ID)
		}
		if report.Confusion[item.ExpectedStatus] == nil {
			report.Confusion[item.ExpectedStatus] = map[Status]int{}
		}
		report.Confusion[item.ExpectedStatus][observation.ActualStatus]++
		if item.Category == CategoryStale {
			staleTotal++
			if observation.ActualStatus == StatusStale {
				staleCorrect++
			}
		}
		if observation.ActualStatus == StatusVerified {
			verifiedPredictions++
			if item.Category == CategoryCurrent {
				verifiedCurrent++
			}
		}
		if item.Category == CategorySubjective {
			subjectiveTotal++
			if !observation.Searched {
				subjectiveBypassed++
			}
		}
		if item.Category == CategoryCurrent || item.Category == CategorySubjective {
			validTotal++
			if observation.Blocked {
				falseBlocks++
			}
		}
		latencies = append(latencies, observation.LatencyMS)
		queryTotal += observation.TavilyQueries
	}

	report.StaleRecall = ratio(staleCorrect, staleTotal)
	report.CurrentPrecision = ratio(verifiedCurrent, verifiedPredictions)
	report.SubjectiveBypass = ratio(subjectiveBypassed, subjectiveTotal)
	report.FalseBlockRate = ratio(falseBlocks, validTotal)
	report.AverageTavilyQueries = ratio(queryTotal, len(cases))
	sort.Float64s(latencies)
	if len(latencies) > 0 {
		index := int(math.Ceil(float64(len(latencies))*0.95)) - 1
		report.P95LatencyMS = latencies[index]
	}
	return report, nil
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}
