package sanitizer_test

import (
	"testing"

	"github.com/mlimarenko/grafana-problem-desk/pkg/sanitizer"
)

func TestNormalizePlainText_PreservesLiteralCodeAndOperators(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text with whitespace trim",
			input:    "  This is normal text  \n",
			expected: "This is normal text",
		},
		{
			name:     "operator characters preserved",
			input:    "load < 2 > 1 and memory >= 90%",
			expected: "load < 2 > 1 and memory >= 90%",
		},
		{
			name:     "code and debugging snippet preserved",
			input:    "Debug snippet: <script>console.log('test')</script>",
			expected: "Debug snippet: <script>console.log('test')</script>",
		},
		{
			name:     "HTML markup preserved as literal text",
			input:    "Service <b>critical</b> in <cluster>",
			expected: "Service <b>critical</b> in <cluster>",
		},
		{
			name:     "entities and symbols preserved verbatim",
			input:    "Tom & Jerry <3 cheese & \"crackers\"",
			expected: "Tom & Jerry <3 cheese & \"crackers\"",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizer.NormalizePlainText(tc.input)
			if got != tc.expected {
				t.Errorf("NormalizePlainText(%q) = %q, expected %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestSanitizeURL_RejectsCredentialsAndInvalidSchemes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid http URL",
			input:    "http://prometheus.example.com/alerts",
			expected: "http://prometheus.example.com/alerts",
		},
		{
			name:     "valid https URL",
			input:    "https://grafana.example.com:3000/alerting",
			expected: "https://grafana.example.com:3000/alerting",
		},
		{
			name:     "credential-bearing URL strictly rejected",
			input:    "http://user:secret@prometheus.example.com/alerts",
			expected: "",
		},
		{
			name:     "javascript URI rejected",
			input:    "javascript:alert(1)",
			expected: "",
		},
		{
			name:     "data URI rejected",
			input:    "data:text/html,<script>alert(1)</script>",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizer.SanitizeURL(tc.input)
			if got != tc.expected {
				t.Errorf("SanitizeURL(%q) = %q, expected %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestNormalizeMap_PreservesIdentity(t *testing.T) {
	input := map[string]string{
		"alertname": "Disk<90>",
		"condition": "load > 5",
	}

	got := sanitizer.NormalizeMap(input)
	for k, expectedVal := range input {
		if got[k] != expectedVal {
			t.Errorf("key %q: got %q, expected %q", k, got[k], expectedVal)
		}
	}
}
