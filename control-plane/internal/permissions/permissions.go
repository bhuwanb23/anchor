package permissions

import (
	"database/sql"
	"net/http"

	"github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

// Role represents a team member role with a permission level.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// roleLevel defines the hierarchy level for each role.
var roleLevel = map[Role]int{
	RoleOwner:  3,
	RoleAdmin:  2,
	RoleMember: 1,
}

// Permission represents an action that can be checked.
type Permission string

const (
	PermManageTeam     Permission = "manage_team"
	PermInviteMembers  Permission = "invite_members"
	PermRemoveMembers  Permission = "remove_members"
	PermManageServers  Permission = "manage_servers"
	PermViewServers    Permission = "view_servers"
	PermTriggerBackups Permission = "trigger_backups"
	PermDeleteTeam     Permission = "delete_team"
)

// rolePermissions defines what each role can do.
var rolePermissions = map[Role][]Permission{
	RoleOwner: {
		PermManageTeam,
		PermInviteMembers,
		PermRemoveMembers,
		PermManageServers,
		PermViewServers,
		PermTriggerBackups,
		PermDeleteTeam,
	},
	RoleAdmin: {
		PermManageTeam,
		PermInviteMembers,
		PermRemoveMembers,
		PermManageServers,
		PermViewServers,
		PermTriggerBackups,
	},
	RoleMember: {
		PermViewServers,
		PermTriggerBackups,
	},
}

// GetUserTeamRole returns the user's role in the given team.
func GetUserTeamRole(db *sql.DB, teamID, userID string) (Role, error) {
	roleStr, err := queries.GetUserTeamRole(db, teamID, userID)
	if err != nil {
		return "", err
	}
	if roleStr == "" {
		return "", nil
	}
	return Role(roleStr), nil
}

// HasMinimumRole checks if the given role meets or exceeds the required role level.
func HasMinimumRole(role Role, required Role) bool {
	roleLvl, ok := roleLevel[role]
	if !ok {
		return false
	}
	reqLvl, ok := roleLevel[required]
	if !ok {
		return false
	}
	return roleLvl >= reqLvl
}

// CanPerform checks if a role has the given permission.
func CanPerform(role Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

// RequireRole checks if the authenticated user has the required role in the team.
// Returns the user's role if authorized, or writes an error response and returns empty string.
func RequireRole(w http.ResponseWriter, r *http.Request, db *sql.DB, teamID string, required Role) Role {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return ""
	}

	role, err := GetUserTeamRole(db, teamID, userID)
	if err != nil {
		http.Error(w, `{"error":"failed to check team membership"}`, http.StatusInternalServerError)
		return ""
	}

	if role == "" {
		http.Error(w, `{"error":"not a team member"}`, http.StatusForbidden)
		return ""
	}

	if !HasMinimumRole(role, required) {
		http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
		return ""
	}

	return role
}

// RequirePermission is a convenience wrapper that checks a specific permission.
func RequirePermission(w http.ResponseWriter, r *http.Request, db *sql.DB, teamID string, perm Permission) Role {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return ""
	}

	role, err := GetUserTeamRole(db, teamID, userID)
	if err != nil {
		http.Error(w, `{"error":"failed to check team membership"}`, http.StatusInternalServerError)
		return ""
	}

	if role == "" {
		http.Error(w, `{"error":"not a team member"}`, http.StatusForbidden)
		return ""
	}

	if !CanPerform(role, perm) {
		http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
		return ""
	}

	return role
}
