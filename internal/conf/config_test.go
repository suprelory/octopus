package conf

import "testing"

func TestMaxRequestBodyBytes(t *testing.T) {
	previous := AppConfig.Server.MaxRequestBodyMB
	t.Cleanup(func() { AppConfig.Server.MaxRequestBodyMB = previous })

	for _, tc := range []struct {
		mb   int
		want int64
	}{
		{-1, 0},
		{0, 0},
		{1, 1 << 20},
		{32, 32 << 20},
	} {
		AppConfig.Server.MaxRequestBodyMB = tc.mb
		if got := MaxRequestBodyBytes(); got != tc.want {
			t.Errorf("MaxRequestBodyBytes() with %d MB = %d, want %d", tc.mb, got, tc.want)
		}
	}
}
