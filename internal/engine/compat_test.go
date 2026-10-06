package engine

import "testing"

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"4.3.9", "4.3.23", -1}, {"v4.3.23", "4.3.23", 0}, {"1.10.0", "1.9.9", 1},
		{"4.0.0-beta.400", "4.0.0-beta.401", -1}, {"1.5", "1.5.0", 0},
	} {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("%s vs %s = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
