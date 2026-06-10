package marketdata

import "testing"

func TestNormalizeHKCode(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"700", "00700"},
		{"00700", "00700"},
		{"HK02800", "02800"},
		{" 700 ", "00700"},
	}
	for _, tc := range tests {
		if got := NormalizeHKCode(tc.in); got != tc.want {
			t.Fatalf("NormalizeHKCode(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
