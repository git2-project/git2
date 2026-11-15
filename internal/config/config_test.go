package config

import (
	"strings"
	"testing"
)

func TestExternalURL(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://localhost/test")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))

	for _, tt := range []struct{ in, want, err string }{
		{"", "", ""},
		{"https://git.example.com", "https://git.example.com", ""},
		{"https://git.example.com/", "https://git.example.com", ""},
		{"http://localhost:5173", "http://localhost:5173", ""},
		{"https://git.example.com/git2", "", "GIT2_EXTERNAL_URL"},
		{"https://git.example.com?x=1", "", "GIT2_EXTERNAL_URL"},
		{"ftp://git.example.com", "", "GIT2_EXTERNAL_URL"},
		{"git.example.com", "", "GIT2_EXTERNAL_URL"},
	} {
		t.Setenv("GIT2_EXTERNAL_URL", tt.in)
		cfg, err := Load()
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%q: err %v, want one naming %s", tt.in, err, tt.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tt.in, err)
			continue
		}
		got := ""
		if cfg.ExternalURL != nil {
			got = cfg.ExternalURL.String()
		}
		if got != tt.want {
			t.Errorf("%q: ExternalURL = %q, want %q", tt.in, got, tt.want)
		}
	}
}
