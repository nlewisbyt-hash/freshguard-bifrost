package main

import (
	"reflect"
	"testing"
)

func TestStringSliceValueAcceptsDecodedJSONArrays(t *testing.T) {
	raw := map[string]any{
		"trusted_domains": []any{" hackculture.io ", 42, "docs.example.com", ""},
	}
	want := []string{"hackculture.io", "docs.example.com"}
	if got := stringSliceValue(raw, "trusted_domains"); !reflect.DeepEqual(got, want) {
		t.Fatalf("stringSliceValue() = %#v, want %#v", got, want)
	}
}

func TestStringSliceValueAcceptsNativeStringSlices(t *testing.T) {
	raw := map[string]any{"trusted_domains": []string{"one.example", " two.example "}}
	want := []string{"one.example", "two.example"}
	if got := stringSliceValue(raw, "trusted_domains"); !reflect.DeepEqual(got, want) {
		t.Fatalf("stringSliceValue() = %#v, want %#v", got, want)
	}
}
