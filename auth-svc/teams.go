package authsvc

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Teams and team members live in auth_svc (004_auth_roles_teams.sql), next
// to the users they group. admin-api manages them through this file.

var (
	ErrTeamNotFound   = errors.New("team not found")
	ErrTeamExists     = errors.New("team name already taken")
	ErrMemberNotFound = errors.New("user is not a member of the team")
)

const maxTeamDescription = 500

// CheckTeamName validates a team name: the same lowercase slug as a
// username (teams_name_slug). Unlike usernames it is not lowercased for the
// caller, and it is not checked against the reserved top-level paths:
// teams do not get a page at /<name>.
func CheckTeamName(s string) error {
	if !usernamePattern.MatchString(s) {
		return &InputError{msg: "name must be 3-39 characters of a-z, 0-9, '-' or '_', starting with a letter or digit"}
	}
	return nil
}

// CheckTeamDescription allows up to 500 characters, the column's limit.
func CheckTeamDescription(s string) error {
	if utf8.RuneCountInString(s) > maxTeamDescription {
		return &InputError{msg: "description must be at most 500 characters"}
	}
	return nil
}

// ParseTeamRole accepts the team roles of roles.go only.
func ParseTeamRole(s string) (Role, error) {
	switch r := Role(s); r {
	case RoleMaintainer, RoleMember:
		return r, nil
	}
	return "", &InputError{msg: "role must be maintainer or member"}
}

// RoleInfo is one row of auth_svc.roles.
type RoleInfo struct {
	Name        Role
	Scope       string // "instance" or "team"
	Description string
}

// ListRoles returns the fixed role list, instance roles first.
func (s *PostgresStore) ListRoles(ctx context.Context) ([]RoleInfo, error) {
	rows, err := s.db.Query(ctx, `SELECT name, scope, description FROM auth_svc.roles ORDER BY scope, name`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	roles, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (RoleInfo, error) {
		var r RoleInfo
		err := row.Scan(&r.Name, &r.Scope, &r.Description)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	return roles, nil
}

type Team struct {
	ID          uuid.UUID
	Name        string
	Description string
	MemberCount int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const teamColumns = `t.id, t.name, t.description,
	(SELECT count(*) FROM auth_svc.team_members m WHERE m.team_id = t.id),
	t.created_at, t.updated_at`

func scanTeam(row pgx.Row) (Team, error) {
	var t Team
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.MemberCount, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

// TeamFilter selects a page of teams, oldest first (ids are UUIDv7).
type TeamFilter struct {
	Prefix string    // start of the name, compared as given (names are lowercase)
	After  uuid.UUID // last id of the previous page; uuid.Nil for the first
	Limit  int
}

func (s *PostgresStore) ListTeams(ctx context.Context, f TeamFilter) ([]Team, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+teamColumns+` FROM auth_svc.teams t
		WHERE t.id > $1 AND ($2 = '' OR t.name LIKE $2 || '%' ESCAPE '\')
		ORDER BY t.id
		LIMIT $3`, f.After, escapeLike(f.Prefix), f.Limit)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	teams, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Team, error) { return scanTeam(row) })
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	return teams, nil
}

// CreateTeam takes a name checked with CheckTeamName. ErrTeamExists.
func (s *PostgresStore) CreateTeam(ctx context.Context, name, description string) (Team, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Team{}, fmt.Errorf("generate team id: %w", err)
	}
	var t Team
	err = s.db.QueryRow(ctx, `
		INSERT INTO auth_svc.teams (id, name, description) VALUES ($1, $2, $3)
		RETURNING id, name, description, created_at, updated_at`, id, name, description).
		Scan(&t.ID, &t.Name, &t.Description, &t.CreatedAt, &t.UpdatedAt)
	if isUniqueViolation(err, "teams_name_key") {
		return Team{}, ErrTeamExists
	}
	if err != nil {
		return Team{}, fmt.Errorf("insert team: %w", err)
	}
	return t, nil
}

// FindTeam returns the team with its member count. ErrTeamNotFound.
func (s *PostgresStore) FindTeam(ctx context.Context, id uuid.UUID) (Team, error) {
	t, err := scanTeam(s.db.QueryRow(ctx, `SELECT `+teamColumns+` FROM auth_svc.teams t WHERE t.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Team{}, ErrTeamNotFound
	}
	if err != nil {
		return Team{}, fmt.Errorf("find team: %w", err)
	}
	return t, nil
}

// TeamUpdate holds the fields an admin may change; nil leaves one as it is.
type TeamUpdate struct {
	Name        *string // checked with CheckTeamName
	Description *string
}

// UpdateTeam applies u and returns the team. ErrTeamExists, ErrTeamNotFound.
func (s *PostgresStore) UpdateTeam(ctx context.Context, id uuid.UUID, u TeamUpdate) (Team, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE auth_svc.teams
		SET name = COALESCE($2, name), description = COALESCE($3, description), updated_at = now()
		WHERE id = $1`, id, u.Name, u.Description)
	if isUniqueViolation(err, "teams_name_key") {
		return Team{}, ErrTeamExists
	}
	if err != nil {
		return Team{}, fmt.Errorf("update team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Team{}, ErrTeamNotFound
	}
	return s.FindTeam(ctx, id)
}

// DeleteTeam deletes the team; its memberships go with it (ON DELETE
// CASCADE). ErrTeamNotFound.
func (s *PostgresStore) DeleteTeam(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM auth_svc.teams WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTeamNotFound
	}
	return nil
}

// TeamMember is one membership, with the member's username.
type TeamMember struct {
	UserID   uuid.UUID
	Username string
	Role     Role
	AddedAt  time.Time
}

// ListTeamMembers returns at most limit members of the team, by username,
// starting after the username afterName ("" for the first page). An
// unknown team has no members; callers look the team up first.
func (s *PostgresStore) ListTeamMembers(ctx context.Context, teamID uuid.UUID, afterName string, limit int) ([]TeamMember, error) {
	rows, err := s.db.Query(ctx, `
		SELECT u.id, u.username, m.role, m.added_at
		FROM auth_svc.team_members m JOIN auth_svc.users u ON u.id = m.user_id
		WHERE m.team_id = $1 AND u.username > $2
		ORDER BY u.username
		LIMIT $3`, teamID, afterName, limit)
	if err != nil {
		return nil, fmt.Errorf("list team members: %w", err)
	}
	members, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (TeamMember, error) {
		var m TeamMember
		err := row.Scan(&m.UserID, &m.Username, &m.Role, &m.AddedAt)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("list team members: %w", err)
	}
	return members, nil
}

// PutTeamMember adds the user to the team with role, or changes the role of
// an existing membership. added reports whether the user was added.
// ErrTeamNotFound, ErrNotFound (no such user).
func (s *PostgresStore) PutTeamMember(ctx context.Context, teamID, userID uuid.UUID, role Role) (m TeamMember, added bool, err error) {
	// xmax is 0 on a row this statement inserted and set on one it updated.
	err = s.db.QueryRow(ctx, `
		WITH put AS (
			INSERT INTO auth_svc.team_members (team_id, user_id, role) VALUES ($1, $2, $3)
			ON CONFLICT (team_id, user_id) DO UPDATE SET role = EXCLUDED.role
			RETURNING user_id, role, added_at, xmax = 0 AS added
		)
		SELECT put.user_id, u.username, put.role, put.added_at, put.added
		FROM put JOIN auth_svc.users u ON u.id = put.user_id`, teamID, userID, role).
		Scan(&m.UserID, &m.Username, &m.Role, &m.AddedAt, &added)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign key violation
		if pgErr.ConstraintName == "team_members_team_fkey" {
			return TeamMember{}, false, ErrTeamNotFound
		}
		return TeamMember{}, false, ErrNotFound
	}
	if err != nil {
		return TeamMember{}, false, fmt.Errorf("put team member: %w", err)
	}
	return m, added, nil
}

// DeleteTeamMember removes the user from the team. ErrMemberNotFound.
func (s *PostgresStore) DeleteTeamMember(ctx context.Context, teamID, userID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM auth_svc.team_members WHERE team_id = $1 AND user_id = $2`, teamID, userID)
	if err != nil {
		return fmt.Errorf("delete team member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMemberNotFound
	}
	return nil
}
