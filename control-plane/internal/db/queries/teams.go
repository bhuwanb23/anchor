package queries

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"
)

// Team represents a team record.
type Team struct {
	ID        string
	Name      string
	OwnerID   string
	CreatedAt string
	UpdatedAt string
}

// TeamMember represents a team membership record.
type TeamMember struct {
	ID        string
	TeamID    string
	UserID    string
	Role      string
	InvitedBy sql.NullString
	JoinedAt  string
}

// Invitation represents a pending team invitation.
type Invitation struct {
	ID         string
	TeamID     string
	Email      string
	Role       string
	Token      string
	InvitedBy  string
	CreatedAt  string
	ExpiresAt  string
	AcceptedAt sql.NullString
}

// ---------------------------------------------------------------------------
// Team CRUD
// ---------------------------------------------------------------------------

// CreateTeam creates a new team with the given owner.
func CreateTeam(db *sql.DB, id, name, ownerID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		"INSERT INTO teams (id, name, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		id, name, ownerID, now, now,
	)
	return err
}

// GetTeamByID returns a team by its ID.
func GetTeamByID(db *sql.DB, teamID string) (*Team, error) {
	var t Team
	err := db.QueryRow(
		"SELECT id, name, owner_id, created_at, updated_at FROM teams WHERE id = ?",
		teamID,
	).Scan(&t.ID, &t.Name, &t.OwnerID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTeamsByUser returns all teams the user is a member of.
func ListTeamsByUser(db *sql.DB, userID string) ([]Team, error) {
	rows, err := db.Query(
		`SELECT t.id, t.name, t.owner_id, t.created_at, t.updated_at
		 FROM teams t
		 INNER JOIN team_members tm ON tm.team_id = t.id
		 WHERE tm.user_id = ?
		 ORDER BY t.name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []Team
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name, &t.OwnerID, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		teams = append(teams, t)
	}
	return teams, rows.Err()
}

// UpdateTeamName updates a team's name.
func UpdateTeamName(db *sql.DB, teamID, name string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		"UPDATE teams SET name = ?, updated_at = ? WHERE id = ?",
		name, now, teamID,
	)
	return err
}

// DeleteTeam deletes a team and all its members (cascade).
func DeleteTeam(db *sql.DB, teamID string) error {
	_, err := db.Exec("DELETE FROM teams WHERE id = ?", teamID)
	return err
}

// TransferOwnership transfers team ownership to another member.
func TransferOwnership(db *sql.DB, teamID, newOwnerID string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)

	// Update team owner
	if _, err := tx.Exec(
		"UPDATE teams SET owner_id = ?, updated_at = ? WHERE id = ?",
		newOwnerID, now, teamID,
	); err != nil {
		return err
	}

	// Make new owner an owner role
	if _, err := tx.Exec(
		"UPDATE team_members SET role = 'owner' WHERE team_id = ? AND user_id = ?",
		teamID, newOwnerID,
	); err != nil {
		return err
	}

	return tx.Commit()
}

// ---------------------------------------------------------------------------
// Team membership
// ---------------------------------------------------------------------------

// AddTeamMember adds a user to a team.
func AddTeamMember(db *sql.DB, id, teamID, userID, role, invitedBy string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var invitedByVal sql.NullString
	if invitedBy != "" {
		invitedByVal = sql.NullString{String: invitedBy, Valid: true}
	}
	_, err := db.Exec(
		"INSERT INTO team_members (id, team_id, user_id, role, invited_by, joined_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, teamID, userID, role, invitedByVal, now,
	)
	return err
}

// GetTeamMember returns a specific team membership.
func GetTeamMember(db *sql.DB, teamID, userID string) (*TeamMember, error) {
	var m TeamMember
	err := db.QueryRow(
		"SELECT id, team_id, user_id, role, invited_by, joined_at FROM team_members WHERE team_id = ? AND user_id = ?",
		teamID, userID,
	).Scan(&m.ID, &m.TeamID, &m.UserID, &m.Role, &m.InvitedBy, &m.JoinedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListTeamMembers returns all members of a team.
func ListTeamMembers(db *sql.DB, teamID string) ([]TeamMember, error) {
	rows, err := db.Query(
		`SELECT tm.id, tm.team_id, tm.user_id, tm.role, tm.invited_by, tm.joined_at
		 FROM team_members tm
		 WHERE tm.team_id = ?
		 ORDER BY tm.joined_at`,
		teamID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []TeamMember
	for rows.Next() {
		var m TeamMember
		if err := rows.Scan(&m.ID, &m.TeamID, &m.UserID, &m.Role, &m.InvitedBy, &m.JoinedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// RemoveTeamMember removes a user from a team.
func RemoveTeamMember(db *sql.DB, teamID, userID string) error {
	_, err := db.Exec(
		"DELETE FROM team_members WHERE team_id = ? AND user_id = ?",
		teamID, userID,
	)
	return err
}

// UpdateMemberRole updates a member's role.
func UpdateMemberRole(db *sql.DB, teamID, userID, role string) error {
	_, err := db.Exec(
		"UPDATE team_members SET role = ? WHERE team_id = ? AND user_id = ?",
		role, teamID, userID,
	)
	return err
}

// IsTeamMember checks if a user is a member of a team.
func IsTeamMember(db *sql.DB, teamID, userID string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM team_members WHERE team_id = ? AND user_id = ?",
		teamID, userID,
	).Scan(&count)
	return err == nil && count > 0, err
}

// GetUserTeamRole returns the user's role in a team, or empty string if not a member.
func GetUserTeamRole(db *sql.DB, teamID, userID string) (string, error) {
	var role string
	err := db.QueryRow(
		"SELECT role FROM team_members WHERE team_id = ? AND user_id = ?",
		teamID, userID,
	).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return role, err
}

// ---------------------------------------------------------------------------
// Server-team linking
// ---------------------------------------------------------------------------

// LinkServerToTeam links a server to a team.
func LinkServerToTeam(db *sql.DB, serverID, teamID string) error {
	_, err := db.Exec(
		"INSERT OR IGNORE INTO server_team (server_id, team_id) VALUES (?, ?)",
		serverID, teamID,
	)
	return err
}

// GetServerTeam returns the team a server belongs to.
func GetServerTeam(db *sql.DB, serverID string) (*Team, error) {
	var t Team
	err := db.QueryRow(
		`SELECT t.id, t.name, t.owner_id, t.created_at, t.updated_at
		 FROM teams t
		 INNER JOIN server_team st ON st.team_id = t.id
		 WHERE st.server_id = ?`,
		serverID,
	).Scan(&t.ID, &t.Name, &t.OwnerID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTeamServers returns all server IDs belonging to a team.
func ListTeamServers(db *sql.DB, teamID string) ([]string, error) {
	rows, err := db.Query(
		"SELECT server_id FROM server_team WHERE team_id = ?",
		teamID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListServersByUserID returns all server IDs the user can access via team membership.
func ListServersByUserID(db *sql.DB, userID string) ([]string, error) {
	rows, err := db.Query(
		`SELECT DISTINCT st.server_id
		 FROM server_team st
		 INNER JOIN team_members tm ON tm.team_id = st.team_id
		 WHERE tm.user_id = ?
		 UNION
		 SELECT id FROM servers WHERE user_id = ?`,
		userID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetUserPersonalTeam returns the user's personal team (owner_id = user_id).
func GetUserPersonalTeam(db *sql.DB, userID string) (*Team, error) {
	var t Team
	err := db.QueryRow(
		"SELECT id, name, owner_id, created_at, updated_at FROM teams WHERE owner_id = ? LIMIT 1",
		userID,
	).Scan(&t.ID, &t.Name, &t.OwnerID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// EnsureUserPersonalTeam creates a personal team for the user if one doesn't exist.
// Returns the team ID.
func EnsureUserPersonalTeam(db *sql.DB, userID, userName string) (string, error) {
	team, err := GetUserPersonalTeam(db, userID)
	if err == nil && team != nil {
		return team.ID, nil
	}

	// Create personal team
	teamID := generateUUID()
	teamName := userName + "'s Team"
	if teamName == "'s Team" || teamName == "" {
		teamName = "My Team"
	}
	if err := CreateTeam(db, teamID, teamName, userID); err != nil {
		return "", err
	}

	// Add user as owner
	memberID := generateUUID()
	if err := AddTeamMember(db, memberID, teamID, userID, "owner", ""); err != nil {
		return "", err
	}

	return teamID, nil
}

// ---------------------------------------------------------------------------
// Invitations
// ---------------------------------------------------------------------------

// CreateInvitation creates a new team invitation.
func CreateInvitation(db *sql.DB, id, teamID, email, role, token, invitedBy, expiresAt string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO invitations (id, team_id, email, role, token, invited_by, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, teamID, email, role, token, invitedBy, now, expiresAt,
	)
	return err
}

// GetInvitationByToken returns an invitation by its token.
func GetInvitationByToken(db *sql.DB, token string) (*Invitation, error) {
	var inv Invitation
	err := db.QueryRow(
		`SELECT id, team_id, email, role, token, invited_by, created_at, expires_at, accepted_at
		 FROM invitations WHERE token = ?`,
		token,
	).Scan(&inv.ID, &inv.TeamID, &inv.Email, &inv.Role, &inv.Token,
		&inv.InvitedBy, &inv.CreatedAt, &inv.ExpiresAt, &inv.AcceptedAt)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// GetPendingInvitation returns a pending (unaccepted, unexpired) invitation for a team+email.
func GetPendingInvitation(db *sql.DB, teamID, email string) (*Invitation, error) {
	var inv Invitation
	err := db.QueryRow(
		`SELECT id, team_id, email, role, token, invited_by, created_at, expires_at, accepted_at
		 FROM invitations WHERE team_id = ? AND email = ? AND accepted_at IS NULL AND expires_at > datetime('now')`,
		teamID, email,
	).Scan(&inv.ID, &inv.TeamID, &inv.Email, &inv.Role, &inv.Token,
		&inv.InvitedBy, &inv.CreatedAt, &inv.ExpiresAt, &inv.AcceptedAt)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// AcceptInvitation marks an invitation as accepted.
func AcceptInvitation(db *sql.DB, invitationID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		"UPDATE invitations SET accepted_at = ? WHERE id = ? AND accepted_at IS NULL",
		now, invitationID,
	)
	return err
}

// DeleteExpiredInvitations removes expired invitations.
func DeleteExpiredInvitations(db *sql.DB) (int64, error) {
	result, err := db.Exec(
		"DELETE FROM invitations WHERE expires_at < datetime('now') AND accepted_at IS NULL",
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// EmailHasPendingInvitation checks if an email has any pending invitations.
func EmailHasPendingInvitation(db *sql.DB, email string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM invitations WHERE email = ? AND accepted_at IS NULL AND expires_at > datetime('now')",
		email,
	).Scan(&count)
	return err == nil && count > 0, err
}

// generateUUID generates a UUID v4 using crypto/rand.
func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
