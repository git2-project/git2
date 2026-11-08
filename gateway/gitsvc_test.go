package gateway

import (
	"context"
	"errors"
	"net"
	"testing"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// repoServer answers FindRepository with a fixed response or error.
type repoServer struct {
	gitv1.UnimplementedRepositoryServiceServer
	id  string
	err error
}

func (s *repoServer) FindRepository(context.Context, *gitv1.FindRepositoryRequest) (*gitv1.FindRepositoryResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &gitv1.FindRepositoryResponse{Repository: &gitv1.Repository{Id: s.id}}, nil
}

// gitSvcOver runs srv on an in-memory listener: the real gRPC client and
// status codes, no network.
func gitSvcOver(t *testing.T, srv *repoServer) *GitSvcRepos {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer()
	gitv1.RegisterRepositoryServiceServer(s, srv)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	must(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return &GitSvcRepos{client: gitv1.NewRepositoryServiceClient(conn)}
}

func TestGitSvcReposFind(t *testing.T) {
	want := uuid.New()
	cases := []struct {
		name     string
		srv      *repoServer
		notFound bool // errRepoNotFound: the caller answers 404
		fault    bool // any other error: the caller answers 503
	}{
		{"found", &repoServer{id: want.String()}, false, false},
		{"not found", &repoServer{err: status.Error(codes.NotFound, "no such repository")}, true, false},
		{"name git-svc could never store", &repoServer{err: status.Error(codes.InvalidArgument, "bad name")}, true, false},
		{"git-svc unavailable", &repoServer{err: status.Error(codes.Unavailable, "down")}, false, true},
		{"git-svc internal error", &repoServer{err: status.Error(codes.Internal, "boom")}, false, true},
		{"bad id in the answer", &repoServer{id: "not-a-uuid"}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, err := gitSvcOver(t, c.srv).Find(context.Background(), uuid.New(), "api")
			switch {
			case c.notFound:
				if !errors.Is(err, errRepoNotFound) {
					t.Fatalf("err = %v, want errRepoNotFound", err)
				}
			case c.fault:
				if err == nil || errors.Is(err, errRepoNotFound) {
					t.Fatalf("err = %v, want a fault that is not errRepoNotFound", err)
				}
			default:
				if err != nil || id != want {
					t.Fatalf("Find = %v, %v; want %v", id, err, want)
				}
			}
		})
	}
}
