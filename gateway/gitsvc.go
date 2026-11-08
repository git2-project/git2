package gateway

import (
	"context"
	"fmt"
	"time"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// findTimeout bounds the repository lookup before a Git request starts.
const findTimeout = 5 * time.Second

// GitSvcRepos finds repositories through git-svc's RepositoryService
// (git.v1, contracts/git). git-svc keys repositories by ID and never sees
// names in Smart HTTP (git2-codeplane ADR 0002), so the gateway asks here.
type GitSvcRepos struct {
	client gitv1.RepositoryServiceClient
}

// NewGitSvcRepos connects lazily: the first lookup dials. Plain gRPC, since
// git-svc's port is reachable only on the instance's internal network.
func NewGitSvcRepos(addr string) (*GitSvcRepos, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("git-svc client: %w", err)
	}
	return &GitSvcRepos{client: gitv1.NewRepositoryServiceClient(conn)}, conn, nil
}

// Find returns errRepoNotFound for NOT_FOUND and for INVALID_ARGUMENT, which
// git-svc answers for a name it could never store; every other failure is
// a fault the caller answers with 503.
func (g *GitSvcRepos) Find(ctx context.Context, ownerID uuid.UUID, name string) (uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, findTimeout)
	defer cancel()
	resp, err := g.client.FindRepository(ctx, &gitv1.FindRepositoryRequest{OwnerId: ownerID.String(), Name: name})
	switch status.Code(err) {
	case codes.OK:
	case codes.NotFound, codes.InvalidArgument:
		return uuid.Nil, errRepoNotFound
	default:
		return uuid.Nil, fmt.Errorf("git-svc FindRepository: %w", err)
	}
	id, err := uuid.Parse(resp.GetRepository().GetId())
	if err != nil {
		return uuid.Nil, fmt.Errorf("git-svc returned a bad repository id: %w", err)
	}
	return id, nil
}
