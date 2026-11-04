package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	benchmarkpkg "github.com/phishguard/freshguard-bifrost/internal/benchmark"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("freshguard-benchmark", flag.ContinueOnError)
	flags.SetOutput(stderr)
	casesPath := flags.String("cases", "benchmark/cases.jsonl", "path to the frozen benchmark cases")
	observationsPath := flags.String("observations", "", "path to measured benchmark observations")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *observationsPath == "" {
		fmt.Fprintln(stderr, "-observations is required")
		return 2
	}

	cases, err := benchmarkpkg.LoadCases(*casesPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	observations, err := benchmarkpkg.LoadObservations(*observationsPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	report, err := benchmarkpkg.Evaluate(cases, observations)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "encode benchmark report: %v\n", err)
		return 1
	}
	return 0
}
