package handlers

import (
	"database/sql"
	"testing"

	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

func TestUserToResponse(t *testing.T) {
	u := queries.User{
		ID:           "usr-123",
		Email:        "alice@example.com",
		Name:         "Alice",
		PasswordHash: "should-not-appear",
	}

	resp := UserToResponse(u)

	if resp.ID != "usr-123" {
		t.Errorf("ID = %q, want %q", resp.ID, "usr-123")
	}
	if resp.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", resp.Email, "alice@example.com")
	}
	if resp.Name != "Alice" {
		t.Errorf("Name = %q, want %q", resp.Name, "Alice")
	}
}

func TestServerToResponse_StripsCredentials(t *testing.T) {
	s := queries.Server{
		ID:              "srv-123",
		Name:            "Prod Server",
		Status:          "connected",
		Token:           "should-not-appear",
		AgentID:         sql.NullString{String: "agt-secret", Valid: true},
		AgentSecretHash: sql.NullString{String: "hash-secret", Valid: true},
		IPAddress:       sql.NullString{String: "1.2.3.4", Valid: true},
		OSInfo:          sql.NullString{String: "linux", Valid: true},
		OSVersion:       sql.NullString{String: "22.04", Valid: true},
		Arch:            sql.NullString{String: "amd64", Valid: true},
		RAMMB:           sql.NullInt64{Int64: 2048, Valid: true},
		DiskGB:          sql.NullInt64{Int64: 40, Valid: true},
	}

	resp := ServerToResponse(s)

	if resp.ID != "srv-123" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Name != "Prod Server" {
		t.Errorf("Name = %q", resp.Name)
	}
	if resp.PublicIP != "1.2.3.4" {
		t.Errorf("PublicIP = %q", resp.PublicIP)
	}
	if resp.OS != "linux" {
		t.Errorf("OS = %q", resp.OS)
	}
	if resp.RAMTotalMB != 2048 {
		t.Errorf("RAMTotalMB = %d", resp.RAMTotalMB)
	}
}

func TestTeamToResponse(t *testing.T) {
	team := queries.Team{
		ID:        "team-123",
		Name:      "Dev Team",
		OwnerID:   "usr-123",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-02T00:00:00Z",
	}

	resp := TeamToResponse(team)

	if resp.ID != "team-123" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Name != "Dev Team" {
		t.Errorf("Name = %q", resp.Name)
	}
	if resp.OwnerID != "usr-123" {
		t.Errorf("OwnerID = %q", resp.OwnerID)
	}
}

func TestTeamMemberToResponse(t *testing.T) {
	m := queries.TeamMember{
		ID:       "mem-123",
		UserID:   "usr-456",
		Role:     "member",
		JoinedAt: "2024-01-01T00:00:00Z",
	}

	resp := TeamMemberToResponse(m)

	if resp.UserID != "usr-456" {
		t.Errorf("UserID = %q", resp.UserID)
	}
	if resp.Role != "member" {
		t.Errorf("Role = %q", resp.Role)
	}
}

func TestInvitationToResponse_StripsToken(t *testing.T) {
	i := queries.Invitation{
		ID:        "inv-123",
		Email:     "bob@example.com",
		Role:      "admin",
		Token:     "should-not-appear",
		InvitedBy: "usr-123",
		CreatedAt: "2024-01-01T00:00:00Z",
		ExpiresAt: "2024-01-08T00:00:00Z",
	}

	resp := InvitationToResponse(i)

	if resp.ID != "inv-123" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Email != "bob@example.com" {
		t.Errorf("Email = %q", resp.Email)
	}
}

func TestAppToResponse(t *testing.T) {
	a := queries.App{
		ID:              "app-123",
		ServerID:        "srv-123",
		ProjectName:     "myshop",
		Status:          "running",
		CurrentImage:    sql.NullString{String: "nginx:latest", Valid: true},
		CurrentHostPort: sql.NullInt64{Int64: 8080, Valid: true},
		MemoryLimitMB:   512,
		CpuQuotaPercent: 50,
		AppPort:         3000,
	}

	resp := AppToResponse(a)

	if resp.ProjectName != "myshop" {
		t.Errorf("ProjectName = %q", resp.ProjectName)
	}
	if resp.CurrentImage != "nginx:latest" {
		t.Errorf("CurrentImage = %q", resp.CurrentImage)
	}
	if resp.CurrentHostPort != 8080 {
		t.Errorf("CurrentHostPort = %d", resp.CurrentHostPort)
	}
}

func TestDeploymentToResponse(t *testing.T) {
	d := queries.Deployment{
		ID:       "dep-123",
		ServerID: "srv-123",
		AppName:  "myshop",
		Image:    "nginx:latest",
		Port:     80,
		Status:   "success",
	}

	resp := DeploymentToResponse(d)

	if resp.ID != "dep-123" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Image != "nginx:latest" {
		t.Errorf("Image = %q", resp.Image)
	}
}

func TestEventToResponse(t *testing.T) {
	e := queries.ServerEvent{
		ID:        "evt-123",
		ServerID:  "srv-123",
		EventType: "deploy",
		CreatedAt: "2024-01-01T00:00:00Z",
	}

	resp := EventToResponse(e)

	if resp.ID != "evt-123" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.EventType != "deploy" {
		t.Errorf("EventType = %q", resp.EventType)
	}
}

func TestAlertToResponse(t *testing.T) {
	a := queries.AlertRecord{
		ID:       "alert-123",
		ServerID: "srv-123",
		Severity: "critical",
		Type:     "disk",
		Status:   "active",
		Title:    "Disk full",
		FiredAt:  "2024-01-01T00:00:00Z",
	}

	resp := AlertToResponse(a)

	if resp.ID != "alert-123" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Severity != "critical" {
		t.Errorf("Severity = %q", resp.Severity)
	}
	if resp.Title != "Disk full" {
		t.Errorf("Title = %q", resp.Title)
	}
}

func TestEnvVarKeyToResponse(t *testing.T) {
	k := queries.EnvVarKey{
		ID:        "env-123",
		KeyName:   "DATABASE_URL",
		IsAuto:    true,
		CreatedAt: "2024-01-01T00:00:00Z",
	}

	resp := EnvVarKeyToResponse(k)

	if resp.KeyName != "DATABASE_URL" {
		t.Errorf("KeyName = %q", resp.KeyName)
	}
	if !resp.IsAuto {
		t.Error("IsAuto should be true")
	}
}

func TestProjectDatabaseToResponse(t *testing.T) {
	d := queries.ProjectDatabase{
		ID:          "pdb-123",
		AppID:       "app-123",
		ServerID:    "srv-123",
		ProjectName: "myshop",
		DbType:      "postgres",
		DBVersion:   sql.NullString{String: "15.0", Valid: true},
		DBName:      sql.NullString{String: "myshop_db", Valid: true},
		Status:      "running",
	}

	resp := ProjectDatabaseToResponse(d)

	if resp.DbType != "postgres" {
		t.Errorf("DbType = %q", resp.DbType)
	}
	if resp.DBVersion != "15.0" {
		t.Errorf("DBVersion = %q", resp.DBVersion)
	}
	if resp.DBName != "myshop_db" {
		t.Errorf("DBName = %q", resp.DBName)
	}
}
