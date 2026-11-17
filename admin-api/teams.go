package adminapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/git2-project/git2/internal/httpx"
	"github.com/google/uuid"
)

// TeamStore is what the role and team routes need from auth-svc.
// authsvc.PostgresStore implements it; handler tests use an in-memory fake.
type TeamStore interface {
	ListRoles(ctx context.Context) ([]authsvc.RoleInfo, error)
	ListTeams(ctx context.Context, f authsvc.TeamFilter) ([]authsvc.Team, error)
	CreateTeam(ctx context.Context, name, description string) (authsvc.Team, error)
	FindTeam(ctx context.Context, id uuid.UUID) (authsvc.Team, error)
	UpdateTeam(ctx context.Context, id uuid.UUID, u authsvc.TeamUpdate) (authsvc.Team, error)
	DeleteTeam(ctx context.Context, id uuid.UUID) error
	ListTeamMembers(ctx context.Context, teamID uuid.UUID, afterName string, limit int) ([]authsvc.TeamMember, error)
	PutTeamMember(ctx context.Context, teamID, userID uuid.UUID, role authsvc.Role) (authsvc.TeamMember, bool, error)
	DeleteTeamMember(ctx context.Context, teamID, userID uuid.UUID) error
}

// Role is the contract's Role.
type Role struct {
	Name        authsvc.Role `json:"name"`
	Scope       string       `json:"scope"`
	Description string       `json:"description"`
}

// Team is the contract's Team.
type Team struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	MemberCount int       `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toTeam(t authsvc.Team) Team {
	return Team{ID: t.ID, Name: t.Name, Description: t.Description, MemberCount: t.MemberCount, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
}

type TeamPage struct {
	Teams         []Team `json:"teams"`
	NextPageToken string `json:"next_page_token,omitempty"`
}

type UserRef struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
}

// TeamMember is the contract's TeamMember.
type TeamMember struct {
	User    UserRef      `json:"user"`
	Role    authsvc.Role `json:"role"`
	AddedAt time.Time    `json:"added_at"`
}

func toTeamMember(m authsvc.TeamMember) TeamMember {
	return TeamMember{User: UserRef{ID: m.UserID, Username: m.Username}, Role: m.Role, AddedAt: m.AddedAt}
}

type TeamMemberPage struct {
	Members       []TeamMember `json:"members"`
	NextPageToken string       `json:"next_page_token,omitempty"`
}

type CreateTeamRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type UpdateTeamRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type PutTeamMemberRequest struct {
	Role string `json:"role"`
}

func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.deps.Teams.ListRoles(r.Context())
	if err != nil {
		slog.Error("admin: list roles", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not list roles, try again")
		return
	}
	out := make([]Role, 0, len(roles))
	for _, ro := range roles {
		out = append(out, Role{Name: ro.Name, Scope: ro.Scope, Description: ro.Description})
	}
	httpx.JSON(w, http.StatusOK, map[string][]Role{"roles": out})
}

func (h *Handler) ListTeams(w http.ResponseWriter, r *http.Request) {
	f := authsvc.TeamFilter{Prefix: r.URL.Query().Get("q")}
	if len(f.Prefix) > 39 {
		httpx.Error(w, http.StatusBadRequest, "q is too long")
		return
	}
	var ok bool
	if f.Limit, ok = pageSize(w, r); !ok {
		return
	}
	if v := r.URL.Query().Get("page_token"); v != "" {
		after, err := decodePageToken(v)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid page_token")
			return
		}
		f.After = after
	}

	// One extra row tells whether another page exists.
	size := f.Limit
	f.Limit++
	teams, err := h.deps.Teams.ListTeams(r.Context(), f)
	if err != nil {
		slog.Error("admin: list teams", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not list teams, try again")
		return
	}
	page := TeamPage{Teams: make([]Team, 0, min(len(teams), size))}
	for i, t := range teams {
		if i == size {
			page.NextPageToken = encodePageToken(teams[i-1].ID)
			break
		}
		page.Teams = append(page.Teams, toTeam(t))
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (h *Handler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	var req CreateTeamRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := errors.Join(authsvc.CheckTeamName(req.Name), authsvc.CheckTeamDescription(req.Description)); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := h.deps.Teams.CreateTeam(r.Context(), req.Name, req.Description)
	if !h.teamStoreOK(w, err, "create team") {
		return
	}
	httpx.JSON(w, http.StatusCreated, toTeam(t))
}

func (h *Handler) GetTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "team_id", "team not found")
	if !ok {
		return
	}
	t, err := h.deps.Teams.FindTeam(r.Context(), id)
	if !h.teamStoreOK(w, err, "find team") {
		return
	}
	httpx.JSON(w, http.StatusOK, toTeam(t))
}

func (h *Handler) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "team_id", "team not found")
	if !ok {
		return
	}
	var req UpdateTeamRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == nil && req.Description == nil {
		httpx.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}
	var errs []error
	if req.Name != nil {
		errs = append(errs, authsvc.CheckTeamName(*req.Name))
	}
	if req.Description != nil {
		errs = append(errs, authsvc.CheckTeamDescription(*req.Description))
	}
	if err := errors.Join(errs...); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := h.deps.Teams.UpdateTeam(r.Context(), id, authsvc.TeamUpdate{Name: req.Name, Description: req.Description})
	if !h.teamStoreOK(w, err, "update team") {
		return
	}
	httpx.JSON(w, http.StatusOK, toTeam(t))
}

func (h *Handler) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "team_id", "team not found")
	if !ok {
		return
	}
	if !h.teamStoreOK(w, h.deps.Teams.DeleteTeam(r.Context(), id), "delete team") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListTeamMembers(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "team_id", "team not found")
	if !ok {
		return
	}
	size, ok := pageSize(w, r)
	if !ok {
		return
	}
	var after string
	if v := r.URL.Query().Get("page_token"); v != "" {
		var err error
		if after, err = decodeNameToken(v); err != nil || after == "" {
			httpx.Error(w, http.StatusBadRequest, "invalid page_token")
			return
		}
	}
	// An unknown team is a 404, not an empty list.
	if _, err := h.deps.Teams.FindTeam(r.Context(), id); !h.teamStoreOK(w, err, "find team") {
		return
	}
	members, err := h.deps.Teams.ListTeamMembers(r.Context(), id, after, size+1)
	if err != nil {
		slog.Error("admin: list team members", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not list members, try again")
		return
	}
	page := TeamMemberPage{Members: make([]TeamMember, 0, min(len(members), size))}
	for i, m := range members {
		if i == size {
			page.NextPageToken = encodeNameToken(members[i-1].Username)
			break
		}
		page.Members = append(page.Members, toTeamMember(m))
	}
	httpx.JSON(w, http.StatusOK, page)
}

// PutTeamMember adds a user to a team or changes their team role. It answers
// 201 when the user was added and 200 otherwise, so repeating it is safe.
func (h *Handler) PutTeamMember(w http.ResponseWriter, r *http.Request) {
	teamID, ok := pathID(w, r, "team_id", "team not found")
	if !ok {
		return
	}
	userID, ok := pathID(w, r, "user_id", "user not found")
	if !ok {
		return
	}
	var req PutTeamMemberRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role, err := authsvc.ParseTeamRole(req.Role)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	m, added, err := h.deps.Teams.PutTeamMember(r.Context(), teamID, userID, role)
	if !h.teamStoreOK(w, err, "put team member") {
		return
	}
	status := http.StatusOK
	if added {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, toTeamMember(m))
}

func (h *Handler) DeleteTeamMember(w http.ResponseWriter, r *http.Request) {
	teamID, ok := pathID(w, r, "team_id", "team not found")
	if !ok {
		return
	}
	userID, ok := pathID(w, r, "user_id", "user not found")
	if !ok {
		return
	}
	if !h.teamStoreOK(w, h.deps.Teams.DeleteTeamMember(r.Context(), teamID, userID), "delete team member") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// teamStoreOK answers the errors the team store can return and reports
// whether err was nil.
func (h *Handler) teamStoreOK(w http.ResponseWriter, err error, op string) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, authsvc.ErrTeamNotFound):
		httpx.Error(w, http.StatusNotFound, "team not found")
	case errors.Is(err, authsvc.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "user not found")
	case errors.Is(err, authsvc.ErrMemberNotFound):
		httpx.Error(w, http.StatusNotFound, "user is not a member of the team")
	case errors.Is(err, authsvc.ErrTeamExists):
		httpx.Error(w, http.StatusConflict, "team name already taken")
	default:
		slog.Error("admin: "+op, "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "the database could not be reached, try again")
	}
	return false
}
