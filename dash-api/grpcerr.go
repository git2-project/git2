package dashapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/git2-project/git2/internal/httpx"
	"github.com/go-chi/chi/v5/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// rpcTimeout bounds each unary call to git-svc. Streams are bounded by the
// request's own deadline instead.
//
// It must leave git-svc room for its own budgets: GetTree lists the
// directory and then gives last_commit 10 s of its own, answering without
// it when that runs out, but only while the caller's deadline holds (ADR
// 0013). 25 s covers that and a long ListCommits with a path, and stays
// below git-svc's 30 s per git call and the router's 30 s Timeout.
const rpcTimeout = 25 * time.Second

func rpcContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), rpcTimeout)
}

// writeGitError answers a failed git-svc call. It is the only place where
// gRPC codes become HTTP statuses:
//
//	NOT_FOUND                                  404
//	INVALID_ARGUMENT                           400
//	ALREADY_EXISTS, FAILED_PRECONDITION        409
//	UNAVAILABLE, DEADLINE_EXCEEDED             503
//	UNAUTHENTICATED, PERMISSION_DENIED         503 (git-svc refused dash-api itself, not the caller)
//	anything else                              500
//
// For 4xx the message is git-svc's, which never holds internal details
// (git.v1 conventions). A fault is logged and answered with a generic
// message. Never 401: clients drop their tokens on a 401.
func writeGitError(w http.ResponseWriter, r *http.Request, err error) {
	st := status.Convert(err)
	msg := st.Message()
	switch st.Code() {
	case codes.NotFound:
		httpx.Error(w, http.StatusNotFound, orDefault(msg, "not found"))
	case codes.InvalidArgument:
		httpx.Error(w, http.StatusBadRequest, orDefault(msg, "invalid request"))
	case codes.AlreadyExists, codes.FailedPrecondition:
		httpx.Error(w, http.StatusConflict, orDefault(msg, "conflict"))
	case codes.Unavailable, codes.DeadlineExceeded, codes.Unauthenticated, codes.PermissionDenied:
		logFault(r, "git-svc call failed", err)
		httpx.Error(w, http.StatusServiceUnavailable, "git service unavailable")
	default:
		logFault(r, "git-svc call failed", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
	}
}

// writeUnavailable answers a fault outside git-svc, such as auth-svc's
// database, with 503.
func writeUnavailable(w http.ResponseWriter, r *http.Request, what string, err error) {
	logFault(r, what, err)
	httpx.Error(w, http.StatusServiceUnavailable, "service unavailable, try again")
}

// logFault logs at error level, unless the client went away first: then the
// failure is the cancellation, not a fault.
func logFault(r *http.Request, msg string, err error) {
	level := slog.LevelError
	if errors.Is(r.Context().Err(), context.Canceled) {
		level = slog.LevelInfo
	}
	slog.Log(r.Context(), level, "dash-api: "+msg, "request_id", middleware.GetReqID(r.Context()), "err", err)
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
