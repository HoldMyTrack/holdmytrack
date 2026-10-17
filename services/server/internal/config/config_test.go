package config

import "testing"

func TestRateLimitsOff(t *testing.T) {
	t.Setenv("S3_ENDPOINT", "http://rustfs:9000")
	cases := []struct {
		value, baseURL string
		off, fails     bool
	}{
		{"", "https://holdmytrack.app", false, false},
		{"on", "https://holdmytrack.app", false, false},
		{"off", "http://localhost:5173", true, false},
		{"off", "http://127.0.0.1:8080", true, false},
		{"off", "http://[::1]:8080", true, false},
		{"off", "https://holdmytrack.app", false, true},
		{"off", "http://localhost.holdmytrack.app", false, true},
		{"no", "http://localhost:5173", false, true},
	}
	for _, c := range cases {
		t.Setenv("RATE_LIMITS", c.value)
		t.Setenv("APP_BASE_URL", c.baseURL)
		cfg, err := Load()
		if (err != nil) != c.fails {
			t.Errorf("RATE_LIMITS=%q at %s: error %v, want failure %v", c.value, c.baseURL, err, c.fails)
			continue
		}
		if err == nil && cfg.RateLimitsOff != c.off {
			t.Errorf("RATE_LIMITS=%q at %s: off %v, want %v", c.value, c.baseURL, cfg.RateLimitsOff, c.off)
		}
	}
}
