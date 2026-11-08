package gateway

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/git2-project/git2/internal/httpx"
)

// readyTimeout bounds each upstream check of /readyz.
const readyTimeout = 2 * time.Second

// readyz answers 200 when every upstream's /healthz does, else 503 with the
// names of the ones that did not (contract, "Routes": GET /readyz). /healthz
// stays the liveness check: a restart does not fix a down upstream.
func readyz(upstreams map[string]*url.URL) http.Handler {
	client := &http.Client{Timeout: readyTimeout}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failing := []string{}
		for name, u := range upstreams {
			if !healthy(r.Context(), client, u) {
				failing = append(failing, name)
			}
		}
		if len(failing) > 0 {
			httpx.JSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not ready", "failing": failing})
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
}

func healthy(ctx context.Context, client *http.Client, base *url.URL) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.JoinPath("healthz").String(), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
