package dashapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWriteGitError(t *testing.T) {
	cases := []struct {
		err      error
		wantCode int
		wantMsg  string
	}{
		{status.Error(codes.NotFound, "ref not found"), http.StatusNotFound, "ref not found"},
		{status.Error(codes.NotFound, ""), http.StatusNotFound, "not found"},
		{status.Error(codes.InvalidArgument, "path must not end with /"), http.StatusBadRequest, "path must not end with /"},
		{status.Error(codes.AlreadyExists, "repository exists"), http.StatusConflict, "repository exists"},
		{status.Error(codes.FailedPrecondition, "not empty"), http.StatusConflict, "not empty"},
		{status.Error(codes.Unavailable, "dial tcp: refused"), http.StatusServiceUnavailable, "git service unavailable"},
		{status.Error(codes.DeadlineExceeded, "slow"), http.StatusServiceUnavailable, "git service unavailable"},
		// git-svc refusing dash-api is a fault behind dash-api, never the caller's 401 or 403.
		{status.Error(codes.Unauthenticated, "no credentials"), http.StatusServiceUnavailable, "git service unavailable"},
		{status.Error(codes.PermissionDenied, "denied"), http.StatusServiceUnavailable, "git service unavailable"},
		{status.Error(codes.Internal, "/var/lib/git2/x: broken"), http.StatusInternalServerError, "internal error"},
		{status.Error(codes.Unimplemented, "not yet"), http.StatusInternalServerError, "internal error"},
		{status.Error(codes.Unknown, "?"), http.StatusInternalServerError, "internal error"},
		{errors.New("not a status"), http.StatusInternalServerError, "internal error"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		writeGitError(rec, httptest.NewRequest(http.MethodGet, "/", nil), c.err)
		t.Run(status.Code(c.err).String(), func(t *testing.T) { wantError(t, rec, c.wantCode, c.wantMsg) })
	}
}
