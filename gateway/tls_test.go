package gateway

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHSTSOnlyWhenTheGatewayServesTLS(t *testing.T) {
	on := newFixture(t, func(d *Deps) { d.HSTS = true })
	off := newFixture(t)
	for _, p := range []string{"/", "/api/auth/me", "/healthz", "/missing.js"} {
		if got := on.do(t, http.MethodGet, p, nil).Header().Get("Strict-Transport-Security"); got != hstsValue {
			t.Errorf("TLS %s: HSTS = %q, want %q", p, got, hstsValue)
		}
		if got := off.do(t, http.MethodGet, p, nil).Header().Get("Strict-Transport-Security"); got != "" {
			t.Errorf("plain HTTP %s: HSTS = %q, want none", p, got)
		}
	}
}

func TestRedirectToHTTPS(t *testing.T) {
	external, _ := url.Parse("https://git.example.test")
	h := RedirectToHTTPS(external)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "http://evil.example/deniz/api.git/info/refs?service=git-upload-pack", nil)
		req.Host = "evil.example"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusPermanentRedirect {
			t.Errorf("%s: status %d, want 308 so the method and body are kept", method, rec.Code)
		}
		want := "https://git.example.test/deniz/api.git/info/refs?service=git-upload-pack"
		if got := rec.Header().Get("Location"); got != want {
			t.Errorf("%s: Location = %q, want %q (never the request's Host)", method, got, want)
		}
	}
}

// The gateway refuses TLS 1.1 and older; a TLS 1.2 client gets through.
func TestTLSConfigRefusesOldVersions(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.TLS = TLSConfig()
	srv.StartTLS()
	defer srv.Close()

	client := func(maxVersion uint16) *http.Client {
		tr := srv.Client().Transport.(*http.Transport).Clone()
		tr.TLSClientConfig.MinVersion = tls.VersionTLS10
		tr.TLSClientConfig.MaxVersion = maxVersion
		return &http.Client{Transport: tr}
	}
	if _, err := client(tls.VersionTLS11).Get(srv.URL); err == nil {
		t.Fatal("a TLS 1.1 client was served")
	}
	resp, err := client(tls.VersionTLS12).Get(srv.URL)
	if err != nil {
		t.Fatalf("a TLS 1.2 client was refused: %v", err)
	}
	_ = resp.Body.Close()
}

func TestLoadConfigTLS(t *testing.T) {
	base := map[string]string{
		"JWT_SECRET":   testSecret,
		"POSTGRES_DSN": "postgres://git2@localhost/git2",
	}
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string // "" means it loads
	}{
		{"plain HTTP", nil, ""},
		{"TLS", map[string]string{"GATEWAY_TLS_CERT_FILE": "c.pem", "GATEWAY_TLS_KEY_FILE": "k.pem"}, ""},
		{"TLS with redirect", map[string]string{"GATEWAY_TLS_CERT_FILE": "c.pem", "GATEWAY_TLS_KEY_FILE": "k.pem",
			"GATEWAY_HTTP_REDIRECT_PORT": "8080", "GIT2_EXTERNAL_URL": "https://git.example.test"}, ""},
		{"certificate without key", map[string]string{"GATEWAY_TLS_CERT_FILE": "c.pem"}, "both"},
		{"key without certificate", map[string]string{"GATEWAY_TLS_KEY_FILE": "k.pem"}, "both"},
		{"redirect without TLS", map[string]string{"GATEWAY_HTTP_REDIRECT_PORT": "8080",
			"GIT2_EXTERNAL_URL": "https://git.example.test"}, "needs TLS"},
		{"redirect to plain HTTP", map[string]string{"GATEWAY_TLS_CERT_FILE": "c.pem", "GATEWAY_TLS_KEY_FILE": "k.pem",
			"GATEWAY_HTTP_REDIRECT_PORT": "8080", "GIT2_EXTERNAL_URL": "http://git.example.test"}, "GIT2_EXTERNAL_URL"},
		{"redirect to a path", map[string]string{"GATEWAY_TLS_CERT_FILE": "c.pem", "GATEWAY_TLS_KEY_FILE": "k.pem",
			"GATEWAY_HTTP_REDIRECT_PORT": "8080", "GIT2_EXTERNAL_URL": "https://git.example.test/git2"}, "GIT2_EXTERNAL_URL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, k := range []string{"GATEWAY_TLS_CERT_FILE", "GATEWAY_TLS_KEY_FILE", "GATEWAY_HTTP_REDIRECT_PORT", "GIT2_EXTERNAL_URL"} {
				t.Setenv(k, "")
			}
			for k, v := range base {
				t.Setenv(k, v)
			}
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			cfg, err := LoadConfig()
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("err = %v, want it to load", err)
			case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
				t.Fatalf("err = %v, want one mentioning %q", err, c.wantErr)
			case c.wantErr == "" && c.env["GIT2_EXTERNAL_URL"] != "" && cfg.ExternalURL.String() != c.env["GIT2_EXTERNAL_URL"]:
				t.Fatalf("ExternalURL = %v", cfg.ExternalURL)
			}
		})
	}
}
