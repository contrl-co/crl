package crllint

import "strings"

func codes(source string) []string {
	report := LintSource("t.crl", source, Options{})
	var out []string
	for _, d := range report.Diagnostics {
		out = append(out, string(d.Code))
	}
	return out
}

func has(codes []string, code string) bool {
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}

var _ = strings.TrimSpace
