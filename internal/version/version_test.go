package version

import "testing"

func TestIsRelease(t *testing.T) {
	for v, want := range map[string]bool{
		"v0.1.0":        true,
		"v10.2.33":      true,
		"v0.1.0-rc.1":   true,
		"dev":           false,
		"71eb712":       false,
		"71eb712-dirty": false,
		"v0.1.0-dirty":  false,
		"v0.1.0-4-gabc": false,
		"0.1.0":         false,
		"v0.1":          false,
	} {
		if got := IsRelease(v); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", v, got, want)
		}
	}
}
