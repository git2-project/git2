package authsvc

// Role is one of the fixed roles seeded into auth_svc.roles by
// 004_auth_roles_teams.sql. The names here and the seed must stay the same.
type Role string

// Instance roles: every user has exactly one, in auth_svc.users.role.
const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// Team roles: one per membership, in auth_svc.team_members.role.
const (
	RoleMaintainer Role = "maintainer"
	RoleMember     Role = "member"
)

// Permission is something a role may do. A new permission is a code change
// with tests, never a database row (docs/design/identity-v0.md).
type Permission string

const (
	// PermAdminRead: read the admin plane (users, teams, roles, settings,
	// audit log).
	PermAdminRead Permission = "admin.read"
	// PermAdminWrite: change anything in the admin plane.
	PermAdminWrite Permission = "admin.write"

	// PermTeamManage: rename a team, add and remove its members.
	PermTeamManage Permission = "team.manage"
)

// instancePermissions is the one table of what each instance role may do.
// RoleUser has no entry: signing in to Dash needs no permission, only
// RequireAuth.
var instancePermissions = map[Role][]Permission{
	RoleAdmin: {PermAdminRead, PermAdminWrite},
}

// teamPermissions is the same table for team roles. RoleMember has no entry:
// membership itself is what grants repository access.
var teamPermissions = map[Role][]Permission{
	RoleMaintainer: {PermTeamManage},
}

// Can reports whether the instance role grants p. A role the code does not
// know (for example one added by a newer migration) grants nothing.
func Can(r Role, p Permission) bool { return grants(instancePermissions, r, p) }

// CanInTeam reports whether the team role grants p within that team.
func CanInTeam(r Role, p Permission) bool { return grants(teamPermissions, r, p) }

func grants(table map[Role][]Permission, r Role, p Permission) bool {
	for _, have := range table[r] {
		if have == p {
			return true
		}
	}
	return false
}
