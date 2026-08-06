package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	"github.com/yourname/yourplatform/control-plane/internal/mailer"
	"github.com/yourname/yourplatform/control-plane/internal/permissions"
)

// Teams handles team management endpoints.
type Teams struct {
	DB     *sql.DB
	Mailer mailer.Sender
	Logger *slog.Logger
}

// ListTeams returns all teams the authenticated user belongs to.
func (t *Teams) ListTeams(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	teams, err := queries.ListTeamsByUser(t.DB, userID)
	if err != nil {
		t.Logger.Error("failed to list teams", "error", err, "user_id", userID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list teams"})
		return
	}

	type teamResponse struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		OwnerID   string `json:"owner_id"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}

	resp := make([]teamResponse, len(teams))
	for i, team := range teams {
		resp[i] = teamResponse{
			ID:        team.ID,
			Name:      team.Name,
			OwnerID:   team.OwnerID,
			CreatedAt: team.CreatedAt,
			UpdatedAt: team.UpdatedAt,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// CreateTeam creates a new team with the authenticated user as owner.
func (t *Teams) CreateTeam(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := DecodeJSON(w, r, &req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team name is required"})
		return
	}

	teamID := uuid.New().String()
	if err := queries.CreateTeam(t.DB, teamID, name, userID); err != nil {
		t.Logger.Error("failed to create team", "error", err, "user_id", userID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create team"})
		return
	}

	// Add user as owner
	memberID := uuid.New().String()
	if err := queries.AddTeamMember(t.DB, memberID, teamID, userID, "owner", ""); err != nil {
		t.Logger.Error("failed to add owner to team", "error", err, "team_id", teamID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to add owner to team"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":   teamID,
		"name": name,
	})
}

// GetTeam returns team details.
func (t *Teams) GetTeam(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "teamID")
	if teamID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team ID is required"})
		return
	}

	// Check membership
	if permissions.RequireRole(w, r, t.DB, teamID, permissions.RoleMember) == "" {
		return
	}

	team, err := queries.GetTeamByID(t.DB, teamID)
	if err != nil {
		t.Logger.Error("failed to get team", "error", err, "team_id", teamID)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "team not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"id":         team.ID,
		"name":       team.Name,
		"owner_id":   team.OwnerID,
		"created_at": team.CreatedAt,
		"updated_at": team.UpdatedAt,
	})
}

// UpdateTeam updates a team's name.
func (t *Teams) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "teamID")
	if teamID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team ID is required"})
		return
	}

	// Require admin role
	if permissions.RequireRole(w, r, t.DB, teamID, permissions.RoleAdmin) == "" {
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := DecodeJSON(w, r, &req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team name is required"})
		return
	}

	if err := queries.UpdateTeamName(t.DB, teamID, name); err != nil {
		t.Logger.Error("failed to update team", "error", err, "team_id", teamID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update team"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

// DeleteTeam deletes a team.
func (t *Teams) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "teamID")
	if teamID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team ID is required"})
		return
	}

	// Require owner role
	if permissions.RequireRole(w, r, t.DB, teamID, permissions.RoleOwner) == "" {
		return
	}

	if err := queries.DeleteTeam(t.DB, teamID); err != nil {
		t.Logger.Error("failed to delete team", "error", err, "team_id", teamID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete team"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "team deleted"})
}

// ListMembers returns all members of a team.
func (t *Teams) ListMembers(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "teamID")
	if teamID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team ID is required"})
		return
	}

	// Check membership
	if permissions.RequireRole(w, r, t.DB, teamID, permissions.RoleMember) == "" {
		return
	}

	members, err := queries.ListTeamMembers(t.DB, teamID)
	if err != nil {
		t.Logger.Error("failed to list members", "error", err, "team_id", teamID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list members"})
		return
	}

	type memberResponse struct {
		ID       string `json:"id"`
		UserID   string `json:"user_id"`
		Role     string `json:"role"`
		JoinedAt string `json:"joined_at"`
	}

	resp := make([]memberResponse, len(members))
	for i, m := range members {
		resp[i] = memberResponse{
			ID:       m.ID,
			UserID:   m.UserID,
			Role:     m.Role,
			JoinedAt: m.JoinedAt,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// UpdateMemberRole changes a member's role.
func (t *Teams) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "teamID")
	memberID := chi.URLParam(r, "memberID")
	if teamID == "" || memberID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team ID and member ID are required"})
		return
	}

	// Require admin role
	if permissions.RequireRole(w, r, t.DB, teamID, permissions.RoleAdmin) == "" {
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := DecodeJSON(w, r, &req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	role := strings.TrimSpace(req.Role)
	if role != "admin" && role != "member" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be 'admin' or 'member'"})
		return
	}

	if err := queries.UpdateMemberRole(t.DB, teamID, memberID, role); err != nil {
		t.Logger.Error("failed to update member role", "error", err, "team_id", teamID, "member_id", memberID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update member role"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"role": role})
}

// RemoveMember removes a member from a team.
func (t *Teams) RemoveMember(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "teamID")
	memberID := chi.URLParam(r, "memberID")
	if teamID == "" || memberID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team ID and member ID are required"})
		return
	}

	// Require admin role
	if permissions.RequireRole(w, r, t.DB, teamID, permissions.RoleAdmin) == "" {
		return
	}

	if err := queries.RemoveTeamMember(t.DB, teamID, memberID); err != nil {
		t.Logger.Error("failed to remove member", "error", err, "team_id", teamID, "member_id", memberID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to remove member"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "member removed"})
}

// TransferOwnership transfers team ownership to another member.
func (t *Teams) TransferOwnership(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "teamID")
	if teamID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team ID is required"})
		return
	}

	// Require owner role
	if permissions.RequireRole(w, r, t.DB, teamID, permissions.RoleOwner) == "" {
		return
	}

	var req struct {
		NewOwnerID string `json:"new_owner_id"`
	}
	if err := DecodeJSON(w, r, &req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	newOwnerID := strings.TrimSpace(req.NewOwnerID)
	if newOwnerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new owner ID is required"})
		return
	}

	// Verify new owner is a team member
	exists, err := queries.IsTeamMember(t.DB, teamID, newOwnerID)
	if err != nil {
		t.Logger.Error("failed to check membership", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check membership"})
		return
	}
	if !exists {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new owner must be a team member"})
		return
	}

	if err := queries.TransferOwnership(t.DB, teamID, newOwnerID); err != nil {
		t.Logger.Error("failed to transfer ownership", "error", err, "team_id", teamID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to transfer ownership"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "ownership transferred"})
}

// SendInvitation invites a user to a team.
func (t *Teams) SendInvitation(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "teamID")
	if teamID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team ID is required"})
		return
	}

	// Require admin role
	if permissions.RequireRole(w, r, t.DB, teamID, permissions.RoleAdmin) == "" {
		return
	}

	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := DecodeJSON(w, r, &req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email is required"})
		return
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "member"
	}
	if role != "admin" && role != "member" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be 'admin' or 'member'"})
		return
	}

	// Check for existing pending invitation
	existing, _ := queries.GetPendingInvitation(t.DB, teamID, email)
	if existing != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "invitation already pending"})
		return
	}

	// Get team info for email
	team, err := queries.GetTeamByID(t.DB, teamID)
	if err != nil {
		t.Logger.Error("failed to get team", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get team"})
		return
	}

	// Get inviter info
	inviterID := middleware.UserIDFromContext(r.Context())

	// Create invitation
	invitationID := uuid.New().String()
	token := uuid.New().String()
	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339)

	if err := queries.CreateInvitation(t.DB, invitationID, teamID, email, role, token, inviterID, expiresAt); err != nil {
		t.Logger.Error("failed to create invitation", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create invitation"})
		return
	}

	// Send invitation email
	if t.Mailer != nil {
		inviteLink := "https://yourplatform.com/invite/" + token
		subject := "You've been invited to join " + team.Name
		body := "You've been invited to join the team '" + team.Name + "'.\n\n" +
			"Click the link to accept: " + inviteLink + "\n\n" +
			"This invitation expires in 7 days."

		if err := t.Mailer.Send(email, subject, body); err != nil {
			t.Logger.Error("failed to send invitation email", "error", err, "email", email)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"invitation_id": invitationID,
		"email":         email,
		"role":          role,
	})
}

// AcceptInvitation accepts a team invitation.
func (t *Teams) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token is required"})
		return
	}

	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Get invitation
	inv, err := queries.GetInvitationByToken(t.DB, token)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "invitation not found"})
		return
	}

	// Check if already accepted
	if inv.AcceptedAt.Valid {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "invitation already accepted"})
		return
	}

	// Check expiration
	expiresAt, err := time.Parse(time.RFC3339, inv.ExpiresAt)
	if err != nil || time.Now().After(expiresAt) {
		writeJSON(w, http.StatusGone, map[string]string{"error": "invitation expired"})
		return
	}

	// Add user to team
	memberID := uuid.New().String()
	if err := queries.AddTeamMember(t.DB, memberID, inv.TeamID, userID, inv.Role, inv.InvitedBy); err != nil {
		t.Logger.Error("failed to add member", "error", err, "team_id", inv.TeamID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to join team"})
		return
	}

	// Mark invitation as accepted
	if err := queries.AcceptInvitation(t.DB, inv.ID); err != nil {
		t.Logger.Error("failed to accept invitation", "error", err, "invitation_id", inv.ID)
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "joined team"})
}
