package dashapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	authsvc "github.com/git2-project/git2/auth-svc"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// --- users ---

// memUsers is an in-memory UserDirectory and TeamReader.
type memUsers struct {
	mu    sync.Mutex
	users []authsvc.User
	// fail, when set, is returned by every call to simulate a database fault.
	fail error
	// emailLookups counts FindByEmail calls.
	emailLookups int
}

func (s *memUsers) find(match func(authsvc.User) bool) (authsvc.User, error) {
	if s.fail != nil {
		return authsvc.User{}, s.fail
	}
	for _, u := range s.users {
		if match(u) {
			return u, nil
		}
	}
	return authsvc.User{}, authsvc.ErrNotFound
}

func (s *memUsers) FindByUsername(_ context.Context, name string) (authsvc.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.find(func(u authsvc.User) bool { return u.Username == name })
}

func (s *memUsers) FindByID(_ context.Context, id uuid.UUID) (authsvc.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.find(func(u authsvc.User) bool { return u.ID == id })
}

func (s *memUsers) FindByEmail(_ context.Context, email string) (authsvc.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emailLookups++
	return s.find(func(u authsvc.User) bool { return u.Email == email })
}

func (s *memUsers) UserTeams(context.Context, uuid.UUID) ([]authsvc.UserTeam, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return nil, s.fail
}

func (s *memUsers) setFail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = err
}

// --- git-svc fakes, served over bufconn: real clients and status codes ---

type fakeRepos struct {
	gitv1.UnimplementedRepositoryServiceServer
	mu    sync.Mutex
	repos []*gitv1.Repository
	// find, when set, replaces the lookup in repos.
	find func(*gitv1.FindRepositoryRequest) (*gitv1.FindRepositoryResponse, error)
	// list, create and del answer their RPCs; an unset one answers
	// UNIMPLEMENTED.
	list   func(*gitv1.ListRepositoriesRequest) (*gitv1.ListRepositoriesResponse, error)
	create func(*gitv1.CreateRepositoryRequest) (*gitv1.CreateRepositoryResponse, error)
	del    func(*gitv1.DeleteRepositoryRequest) (*gitv1.DeleteRepositoryResponse, error)
}

func (f *fakeRepos) ListRepositories(ctx context.Context, req *gitv1.ListRepositoriesRequest) (*gitv1.ListRepositoriesResponse, error) {
	if f.list == nil {
		return f.UnimplementedRepositoryServiceServer.ListRepositories(ctx, req)
	}
	return f.list(req)
}

func (f *fakeRepos) CreateRepository(ctx context.Context, req *gitv1.CreateRepositoryRequest) (*gitv1.CreateRepositoryResponse, error) {
	if f.create == nil {
		return f.UnimplementedRepositoryServiceServer.CreateRepository(ctx, req)
	}
	return f.create(req)
}

func (f *fakeRepos) DeleteRepository(ctx context.Context, req *gitv1.DeleteRepositoryRequest) (*gitv1.DeleteRepositoryResponse, error) {
	if f.del == nil {
		return f.UnimplementedRepositoryServiceServer.DeleteRepository(ctx, req)
	}
	return f.del(req)
}

func (f *fakeRepos) FindRepository(_ context.Context, req *gitv1.FindRepositoryRequest) (*gitv1.FindRepositoryResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.find != nil {
		return f.find(req)
	}
	for _, r := range f.repos {
		if r.GetOwnerId() == req.GetOwnerId() && r.GetName() == req.GetName() {
			return &gitv1.FindRepositoryResponse{Repository: r}, nil
		}
	}
	return nil, status.Error(codes.NotFound, "repository not found")
}

// Each field answers one RPC; an unset one answers UNIMPLEMENTED.
type fakeRefs struct {
	gitv1.UnimplementedRefServiceServer
	listBranches func(*gitv1.ListBranchesRequest) (*gitv1.ListBranchesResponse, error)
	listTags     func(*gitv1.ListTagsRequest) (*gitv1.ListTagsResponse, error)
}

func (f *fakeRefs) ListBranches(ctx context.Context, req *gitv1.ListBranchesRequest) (*gitv1.ListBranchesResponse, error) {
	if f.listBranches == nil {
		return f.UnimplementedRefServiceServer.ListBranches(ctx, req)
	}
	return f.listBranches(req)
}

func (f *fakeRefs) ListTags(ctx context.Context, req *gitv1.ListTagsRequest) (*gitv1.ListTagsResponse, error) {
	if f.listTags == nil {
		return f.UnimplementedRefServiceServer.ListTags(ctx, req)
	}
	return f.listTags(req)
}

type fakeCommits struct {
	gitv1.UnimplementedCommitServiceServer
	getTree     func(*gitv1.GetTreeRequest) (*gitv1.GetTreeResponse, error)
	getBlob     func(*gitv1.GetBlobRequest) (*gitv1.GetBlobResponse, error)
	getRawBlob  func(*gitv1.GetRawBlobRequest, grpc.ServerStreamingServer[gitv1.GetRawBlobResponse]) error
	listCommits func(*gitv1.ListCommitsRequest) (*gitv1.ListCommitsResponse, error)
	getCommit   func(*gitv1.GetCommitRequest) (*gitv1.GetCommitResponse, error)

	mu       sync.Mutex
	deadline time.Time // of the last GetTree or ListCommits, as git-svc sees it
}

// saveDeadline keeps the deadline that dash-api's call carried over gRPC.
func (f *fakeCommits) saveDeadline(ctx context.Context) {
	d, _ := ctx.Deadline()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deadline = d
}

// lastDeadline returns the deadline saved by the last GetTree or ListCommits;
// zero when the call had none.
func (f *fakeCommits) lastDeadline() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deadline
}

func (f *fakeCommits) GetTree(ctx context.Context, req *gitv1.GetTreeRequest) (*gitv1.GetTreeResponse, error) {
	f.saveDeadline(ctx)
	if f.getTree == nil {
		return f.UnimplementedCommitServiceServer.GetTree(ctx, req)
	}
	return f.getTree(req)
}

func (f *fakeCommits) GetBlob(ctx context.Context, req *gitv1.GetBlobRequest) (*gitv1.GetBlobResponse, error) {
	if f.getBlob == nil {
		return f.UnimplementedCommitServiceServer.GetBlob(ctx, req)
	}
	return f.getBlob(req)
}

func (f *fakeCommits) GetRawBlob(req *gitv1.GetRawBlobRequest, s grpc.ServerStreamingServer[gitv1.GetRawBlobResponse]) error {
	if f.getRawBlob == nil {
		return f.UnimplementedCommitServiceServer.GetRawBlob(req, s)
	}
	return f.getRawBlob(req, s)
}

func (f *fakeCommits) ListCommits(ctx context.Context, req *gitv1.ListCommitsRequest) (*gitv1.ListCommitsResponse, error) {
	f.saveDeadline(ctx)
	if f.listCommits == nil {
		return f.UnimplementedCommitServiceServer.ListCommits(ctx, req)
	}
	return f.listCommits(req)
}

func (f *fakeCommits) GetCommit(ctx context.Context, req *gitv1.GetCommitRequest) (*gitv1.GetCommitResponse, error) {
	if f.getCommit == nil {
		return f.UnimplementedCommitServiceServer.GetCommit(ctx, req)
	}
	return f.getCommit(req)
}

// --- harness ---

const testExternalURL = "https://git2.example.com"

type harness struct {
	t       *testing.T
	router  http.Handler
	users   *memUsers
	repos   *fakeRepos
	refs    *fakeRefs
	commits *fakeCommits
	jwt     *authjwt.Manager

	ada, bob           authsvc.User
	adaToken, bobToken string
	// repo is ada/billing-service.
	repo   *gitv1.Repository
	repoID uuid.UUID
}

type harnessOption func(*Deps)

// withAccess replaces the v0 owner-only rule.
func withAccess(a AccessChecker) harnessOption { return func(d *Deps) { d.Access = a } }

func newHarness(t *testing.T, opts ...harnessOption) *harness {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	h := &harness{
		t:       t,
		users:   &memUsers{},
		repos:   &fakeRepos{},
		refs:    &fakeRefs{},
		commits: &fakeCommits{},
		jwt:     authjwt.NewManager("test-secret-that-is-at-least-32-chars", time.Minute, time.Hour),
	}
	h.ada = h.addUser("ada", "ada@example.com")
	h.bob = h.addUser("bob", "bob@example.com")
	h.adaToken = h.token(h.ada)
	h.bobToken = h.token(h.bob)

	h.repoID = uuid.Must(uuid.NewV7())
	h.repo = &gitv1.Repository{
		Id:            h.repoID.String(),
		OwnerId:       h.ada.ID.String(),
		Name:          "billing-service",
		DefaultBranch: "main",
		Description:   "Invoices and payments",
		CreatedAt:     timestamppb.New(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)),
		UpdatedAt:     timestamppb.New(time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)),
	}
	h.repos.repos = []*gitv1.Repository{h.repo}

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	gitv1.RegisterRepositoryServiceServer(srv, h.repos)
	gitv1.RegisterRefServiceServer(srv, h.refs)
	gitv1.RegisterCommitServiceServer(srv, h.commits)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	deps := Deps{
		Users:       h.users,
		Teams:       h.users,
		Repos:       gitv1.NewRepositoryServiceClient(conn),
		Refs:        gitv1.NewRefServiceClient(conn),
		Commits:     gitv1.NewCommitServiceClient(conn),
		JWT:         h.jwt,
		Redis:       rdb,
		ExternalURL: testExternalURL + "/",
	}
	for _, o := range opts {
		o(&deps)
	}
	handler, err := NewHandler(deps)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	// As in cmd/git2: Recoverer passes http.ErrAbortHandler on.
	r.Use(middleware.Recoverer)
	r.Route("/api", handler.Mount)
	h.router = r
	return h
}

func (h *harness) addUser(username, email string) authsvc.User {
	u := authsvc.User{ID: uuid.Must(uuid.NewV7()), Username: username, Email: email, IsActive: true, Role: authsvc.RoleUser}
	h.users.users = append(h.users.users, u)
	return u
}

func (h *harness) token(u authsvc.User) string {
	tok, err := h.jwt.IssueAccess(u.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return tok
}

// get sends a GET with token as the bearer (none when empty).
func (h *harness) get(path, token string) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.do(http.MethodGet, path, token, "")
}

// do sends a request with body as JSON (none when empty) and token as the
// bearer (none when empty).
func (h *harness) do(method, path, token, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

// repoPath is /api/repos/ada/billing-service plus rest.
func repoPath(rest string) string { return "/api/repos/ada/billing-service" + rest }

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, want, rec.Body.String())
	}
}

// wantError checks the status and the {"error": msg} body.
func wantError(t *testing.T, rec *httptest.ResponseRecorder, wantCode int, wantMsg string) {
	t.Helper()
	wantStatus(t, rec, wantCode)
	var body struct{ Error string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	if body.Error != wantMsg {
		t.Fatalf("error = %q, want %q", body.Error, wantMsg)
	}
}

// decode unmarshals the body into a generic map, so tests see the JSON as a
// client does, nulls included.
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return m
}

// --- builders ---

var testTime = time.Date(2026, 11, 12, 16, 40, 0, 0, time.UTC)

func sig(name, email string) *gitv1.Signature {
	return &gitv1.Signature{Name: name, Email: email, Time: timestamppb.New(testTime)}
}

func commit(sha, title string) *gitv1.Commit {
	return &gitv1.Commit{
		Sha:       sha,
		Title:     title,
		Message:   title,
		Author:    sig("Ada Lovelace", "ada@example.com"),
		Committer: sig("Ada Lovelace", "ada@example.com"),
	}
}

const (
	sha1 = "4bdc5531f0e2a7d9c6b8e3f1a2d4c6b8e0f1a3c5"
	sha2 = "9e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f"
)

// levelFunc is an AccessChecker made of a function.
type levelFunc func(userID, repoID, ownerID uuid.UUID) (Level, error)

func (f levelFunc) Level(_ context.Context, userID, repoID, ownerID uuid.UUID) (Level, error) {
	return f(userID, repoID, ownerID)
}

// jsonEqual compares decoded JSON values; numbers are float64.
func jsonEqual(a, b any) bool { return reflect.DeepEqual(a, b) }

// TestMain silences the fault logs the error-path tests produce on purpose.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}
