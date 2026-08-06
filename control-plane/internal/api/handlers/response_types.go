package handlers

import (
	"database/sql"

	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

// ---------------------------------------------------------------------------
// User
// ---------------------------------------------------------------------------

// UserResponse is the API-safe user object. password_hash is never exposed.
type UserResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitempty"`
}

// UserToResponse converts a DB user to an API response.
func UserToResponse(u queries.User) UserResponse {
	return UserResponse{
		ID:    u.ID,
		Email: u.Email,
		Name:  u.Name,
	}
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

// ServerResponse is the API-safe server object.
// agent_secret_hash, token, and agent_id are never exposed.
type ServerResponse struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Status        string   `json:"status"`
	PublicIP      string   `json:"public_ip,omitempty"`
	OS            string   `json:"os,omitempty"`
	OSVersion     string   `json:"os_version,omitempty"`
	Arch          string   `json:"arch,omitempty"`
	CPUCount      int      `json:"cpu_count,omitempty"`
	RAMTotalMB    int64    `json:"ram_total_mb,omitempty"`
	DiskTotalGB   int64    `json:"disk_total_gb,omitempty"`
	ConnectedAt   string   `json:"connected_at,omitempty"`
	LastSeen      string   `json:"last_seen,omitempty"`
	CreatedAt     string   `json:"created_at,omitempty"`
	Metrics       *MetricsSnapshot `json:"metrics,omitempty"`
}

// MetricsSnapshot holds the latest resource usage for a server.
type MetricsSnapshot struct {
	CPUPercent  float64 `json:"cpu_percent"`
	RAMUsedMB   int64   `json:"ram_used_mb"`
	RAMTotalMB  int64   `json:"ram_total_mb"`
	RAMPercent  float64 `json:"ram_percent"`
	DiskUsedGB  float64 `json:"disk_used_gb"`
	DiskTotalGB float64 `json:"disk_total_gb"`
	DiskPercent float64 `json:"disk_percent"`
	Load1Min    float64 `json:"load_1min"`
}

// ServerToResponse converts a DB server to an API response,
// stripping all credential fields.
func ServerToResponse(s queries.Server) ServerResponse {
	resp := ServerResponse{
		ID:          s.ID,
		Name:        s.Name,
		Status:      s.Status,
		ConnectedAt: s.ConnectedAt,
		LastSeen:    s.LastSeen,
	}
	if s.IPAddress.Valid {
		resp.PublicIP = s.IPAddress.String
	}
	if s.OSInfo.Valid {
		resp.OS = s.OSInfo.String
	}
	if s.OSVersion.Valid {
		resp.OSVersion = s.OSVersion.String
	}
	if s.Arch.Valid {
		resp.Arch = s.Arch.String
	}
	if s.RAMMB.Valid {
		resp.RAMTotalMB = s.RAMMB.Int64
	}
	if s.DiskGB.Valid {
		resp.DiskTotalGB = s.DiskGB.Int64
	}
	return resp
}

// ---------------------------------------------------------------------------
// Team
// ---------------------------------------------------------------------------

// TeamResponse is the API-safe team object.
type TeamResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OwnerID   string `json:"owner_id"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// TeamToResponse converts a DB team to an API response.
func TeamToResponse(t queries.Team) TeamResponse {
	return TeamResponse{
		ID:        t.ID,
		Name:      t.Name,
		OwnerID:   t.OwnerID,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// ---------------------------------------------------------------------------
// TeamMember
// ---------------------------------------------------------------------------

// TeamMemberResponse is the API-safe team member object.
type TeamMemberResponse struct {
	ID       string `json:"id"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at,omitempty"`
}

// TeamMemberToResponse converts a DB team member to an API response.
func TeamMemberToResponse(m queries.TeamMember) TeamMemberResponse {
	return TeamMemberResponse{
		ID:       m.ID,
		UserID:   m.UserID,
		Role:     m.Role,
		JoinedAt: m.JoinedAt,
	}
}

// ---------------------------------------------------------------------------
// Invitation
// ---------------------------------------------------------------------------

// InvitationResponse is the API-safe invitation object. The token is never exposed.
type InvitationResponse struct {
	ID        string         `json:"id"`
	Email     string         `json:"email"`
	Role      string         `json:"role"`
	InvitedBy string         `json:"invited_by"`
	CreatedAt string         `json:"created_at,omitempty"`
	ExpiresAt string         `json:"expires_at,omitempty"`
	AcceptedAt sql.NullString `json:"accepted_at,omitempty"`
}

// InvitationToResponse converts a DB invitation to an API response.
func InvitationToResponse(i queries.Invitation) InvitationResponse {
	return InvitationResponse{
		ID:        i.ID,
		Email:     i.Email,
		Role:      i.Role,
		InvitedBy: i.InvitedBy,
		CreatedAt: i.CreatedAt,
		ExpiresAt: i.ExpiresAt,
		AcceptedAt: i.AcceptedAt,
	}
}

// ---------------------------------------------------------------------------
// App
// ---------------------------------------------------------------------------

// AppResponse is the API-safe app object.
type AppResponse struct {
	ID              string  `json:"id"`
	ServerID        string  `json:"server_id"`
	ProjectName     string  `json:"project_name"`
	Status          string  `json:"status"`
	CurrentImage    string  `json:"current_image,omitempty"`
	CurrentHostPort int64   `json:"current_host_port,omitempty"`
	PlatformDomain  string  `json:"platform_domain,omitempty"`
	MemoryLimitMB   int     `json:"memory_limit_mb"`
	CPUQuotaPercent int     `json:"cpu_quota_percent"`
	AppPort         int     `json:"app_port"`
	CreatedAt       string  `json:"created_at,omitempty"`
	UpdatedAt       string  `json:"updated_at,omitempty"`
}

// AppToResponse converts a DB app to an API response.
func AppToResponse(a queries.App) AppResponse {
	resp := AppResponse{
		ID:              a.ID,
		ServerID:        a.ServerID,
		ProjectName:     a.ProjectName,
		Status:          a.Status,
		MemoryLimitMB:   a.MemoryLimitMB,
		CPUQuotaPercent: a.CpuQuotaPercent,
		AppPort:         a.AppPort,
		CreatedAt:       a.CreatedAt,
		UpdatedAt:       a.UpdatedAt,
	}
	if a.CurrentImage.Valid {
		resp.CurrentImage = a.CurrentImage.String
	}
	if a.CurrentHostPort.Valid {
		resp.CurrentHostPort = a.CurrentHostPort.Int64
	}
	if a.PlatformDomain.Valid {
		resp.PlatformDomain = a.PlatformDomain.String
	}
	return resp
}

// ---------------------------------------------------------------------------
// Deployment
// ---------------------------------------------------------------------------

// DeploymentResponse is the API-safe deployment object.
type DeploymentResponse struct {
	ID        string `json:"id"`
	ServerID  string `json:"server_id"`
	AppName   string `json:"app_name"`
	Image     string `json:"image"`
	Port      int    `json:"port"`
	Domain    string `json:"domain,omitempty"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// DeploymentToResponse converts a DB deployment to an API response.
func DeploymentToResponse(d queries.Deployment) DeploymentResponse {
	resp := DeploymentResponse{
		ID:        d.ID,
		ServerID:  d.ServerID,
		AppName:   d.AppName,
		Image:     d.Image,
		Port:      d.Port,
		Status:    d.Status,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
	if d.Domain.Valid {
		resp.Domain = d.Domain.String
	}
	return resp
}

// ---------------------------------------------------------------------------
// Event
// ---------------------------------------------------------------------------

// EventResponse is the API-safe server event object.
type EventResponse struct {
	ID        string `json:"id"`
	ServerID  string `json:"server_id"`
	EventType string `json:"event_type"`
	CheckName string `json:"check_name,omitempty"`
	Message   string `json:"message,omitempty"`
	Details   string `json:"details,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// EventToResponse converts a DB server event to an API response.
func EventToResponse(e queries.ServerEvent) EventResponse {
	resp := EventResponse{
		ID:        e.ID,
		ServerID:  e.ServerID,
		EventType: e.EventType,
		CreatedAt: e.CreatedAt,
	}
	if e.CheckName.Valid {
		resp.CheckName = e.CheckName.String
	}
	if e.Message.Valid {
		resp.Message = e.Message.String
	}
	if e.Details.Valid {
		resp.Details = e.Details.String
	}
	return resp
}

// ---------------------------------------------------------------------------
// Alert
// ---------------------------------------------------------------------------

// AlertResponse is the API-safe alert object. metrics_json is excluded.
type AlertResponse struct {
	ID             string `json:"id"`
	ServerID       string `json:"server_id"`
	ServerName     string `json:"server_name,omitempty"`
	Project        string `json:"project,omitempty"`
	Container      string `json:"container,omitempty"`
	Severity       string `json:"severity"`
	Level          string `json:"level,omitempty"`
	Type           string `json:"type"`
	Status         string `json:"status"`
	Title          string `json:"title,omitempty"`
	Message        string `json:"message,omitempty"`
	Detail         string `json:"detail,omitempty"`
	Action         string `json:"action,omitempty"`
	FiredAt        string `json:"fired_at"`
	ResolvedAt     string `json:"resolved_at,omitempty"`
	ReadAt         string `json:"read_at,omitempty"`
	AcknowledgedAt string `json:"acknowledged_at,omitempty"`
	AcknowledgedBy string `json:"acknowledged_by,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
}

// AlertToResponse converts a DB alert to an API response.
func AlertToResponse(a queries.AlertRecord) AlertResponse {
	return AlertResponse{
		ID:             a.ID,
		ServerID:       a.ServerID,
		ServerName:     a.ServerName,
		Project:        a.Project,
		Container:      a.Container,
		Severity:       a.Severity,
		Level:          a.Level,
		Type:           a.Type,
		Status:         a.Status,
		Title:          a.Title,
		Message:        a.Message,
		Detail:         a.Detail,
		Action:         a.Action,
		FiredAt:        a.FiredAt,
		ResolvedAt:     a.ResolvedAt,
		ReadAt:         a.ReadAt,
		AcknowledgedAt: a.AcknowledgedAt,
		AcknowledgedBy: a.AcknowledgedBy,
		CreatedAt:      a.CreatedAt,
	}
}

// ---------------------------------------------------------------------------
// EnvVarKey
// ---------------------------------------------------------------------------

// EnvVarKeyResponse is the API-safe env var key object. Values are never stored in DB.
type EnvVarKeyResponse struct {
	ID        string `json:"id"`
	KeyName   string `json:"key_name"`
	IsAuto    bool   `json:"is_auto"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// EnvVarKeyToResponse converts a DB env var key to an API response.
func EnvVarKeyToResponse(k queries.EnvVarKey) EnvVarKeyResponse {
	return EnvVarKeyResponse{
		ID:        k.ID,
		KeyName:   k.KeyName,
		IsAuto:    k.IsAuto,
		CreatedAt: k.CreatedAt,
		UpdatedAt: k.UpdatedAt,
	}
}

// ---------------------------------------------------------------------------
// ProjectDatabase
// ---------------------------------------------------------------------------

// ProjectDatabaseResponse is the API-safe project database object.
type ProjectDatabaseResponse struct {
	ID          string `json:"id"`
	AppID       string `json:"app_id"`
	ServerID    string `json:"server_id"`
	ProjectName string `json:"project_name"`
	DbType      string `json:"db_type"`
	DBVersion   string `json:"db_version,omitempty"`
	DBName      string `json:"db_name,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at,omitempty"`
}

// ProjectDatabaseToResponse converts a DB project database to an API response.
func ProjectDatabaseToResponse(d queries.ProjectDatabase) ProjectDatabaseResponse {
	resp := ProjectDatabaseResponse{
		ID:          d.ID,
		AppID:       d.AppID,
		ServerID:    d.ServerID,
		ProjectName: d.ProjectName,
		DbType:      d.DbType,
		Status:      d.Status,
		CreatedAt:   d.CreatedAt,
	}
	if d.DBVersion.Valid {
		resp.DBVersion = d.DBVersion.String
	}
	if d.DBName.Valid {
		resp.DBName = d.DBName.String
	}
	return resp
}
