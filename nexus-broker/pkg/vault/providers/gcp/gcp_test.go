package gcp

import "testing"

func TestVersionOlder(t *testing.T) {
	const base = "projects/p/secrets/s/versions/"
	cases := []struct {
		a, b string
		want bool
	}{
		{base + "1", base + "2", true},
		{base + "2", base + "2", false},
		{base + "3", base + "2", false},
		{base + "9", base + "10", true}, // numeric, not lexical
		{"garbage", base + "2", false},  // unparsable is never retired
		{base + "1", "garbage", false},
	}
	for _, c := range cases {
		if got := versionOlder(c.a, c.b); got != c.want {
			t.Errorf("versionOlder(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
