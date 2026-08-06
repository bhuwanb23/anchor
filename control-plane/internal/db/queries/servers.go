package queries

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"
)

func InsertServer(db *sql.DB, id, userID, name, token string) error {
	_, err := db.Exec(
		"INSERT INTO servers (id, user_id, name, token) VALUES (?, ?, ?, ?)",
		id, userID, name, token,
	)
	return err
}

func QueryServersByUser(db *sql.DB, userID string) (*sql.Rows, error) {
	return db.Query(
		"SELECT id, name, status, connected_at, last_seen FROM servers WHERE user_id = ?",
		userID,
	)
}

func GetServerByToken(db *sql.DB, token string) (id, userID, name string, err error) {
	err = db.QueryRow(
		"SELECT id, user_id, name FROM servers WHERE token = ?",
		token,
	).Scan(&id, &userID, &name)
	return
}

func UpdateServerStatus(db *sql.DB, serverID, status string) error {
	_, err := db.Exec(
		"UPDATE servers SET status = ?, last_seen = ? WHERE id = ?",
		status, time.Now().UTC().Format(time.RFC3339), serverID,
	)
	return err
}

func InsertServerWithAgent(db *sql.DB, id, userID, name, agentID, agentSecretHash, osInfo, arch string, ramMB, diskGB int, ipAddress string) error {
	// The servers table has a NOT NULL token column used by the legacy flow.
	// Agent-registered servers don't use it, but we must provide a unique value.
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	token := "agent_" + hex.EncodeToString(b)
	_, err := db.Exec(
		"INSERT INTO servers (id, user_id, name, token, agent_id, agent_secret_hash, os_info, arch, ram_mb, disk_gb, ip_address, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')",
		id, userID, name, token, agentID, agentSecretHash, osInfo, arch, ramMB, diskGB, ipAddress,
	)
	return err
}

func GetServerByAgentID(db *sql.DB, agentID string) (id, userID, name, agentSecretHash, status string, err error) {
	err = db.QueryRow(
		"SELECT id, user_id, name, agent_secret_hash, status FROM servers WHERE agent_id = ?",
		agentID,
	).Scan(&id, &userID, &name, &agentSecretHash, &status)
	return
}

func UpdateServerConnection(db *sql.DB, serverID, status string) error {
	_, err := db.Exec(
		"UPDATE servers SET status = ?, last_seen = datetime('now') WHERE id = ?",
		status, serverID,
	)
	return err
}

func UpdateServerSystemInfo(db *sql.DB, serverID, osVersion, osPretty, dockerVersion string, ramAvailableMB, diskTotalGB, diskAvailableGB int, diskUsedPercent float64) error {
	_, err := db.Exec(
		`UPDATE servers SET
			os_version = ?, os_pretty = ?,
			ram_available_mb = ?, disk_total_gb = ?, disk_available_gb = ?, disk_used_percent = ?,
			docker_version = ?,
			last_seen = datetime('now')
		WHERE id = ?`,
		osVersion, osPretty,
		ramAvailableMB, diskTotalGB, diskAvailableGB, diskUsedPercent,
		dockerVersion,
		serverID,
	)
	return err
}

func GetServerByID(db *sql.DB, serverID string) (name, ipAddress string, err error) {
	err = db.QueryRow(
		"SELECT name, ip_address FROM servers WHERE id = ?",
		serverID,
	).Scan(&name, &ipAddress)
	return
}

func GetDeploymentByServerAndApp(db *sql.DB, serverID, appName string) (id string, err error) {
	err = db.QueryRow(
		"SELECT id FROM deployments WHERE server_id = ? AND app_name = ? AND status != 'stopped' LIMIT 1",
		serverID, appName,
	).Scan(&id)
	return
}

// GetServerStatus returns the current connection status of a server
// (e.g. "connected", "disconnected"). Used for the server_state snapshot a
// browser receives on subscribe (Layer 5B Step 3B).
func GetServerStatus(db *sql.DB, serverID string) (string, error) {
	var status string
	err := db.QueryRow(
		"SELECT status FROM servers WHERE id = ?",
		serverID,
	).Scan(&status)
	if err != nil {
		return "", err
	}
	return status, nil
}

// ---------------------------------------------------------------------------
// Typed server queries (Layer 5C Step 3A/3C)
// ---------------------------------------------------------------------------

// Server is a full row of the servers table. Nullable columns map to the
// sql.Null* types so callers can distinguish "not reported yet" from zero.
//
// Token and AgentSecretHash are credentials: they are excluded from any JSON
// marshaling (json:"-") so a handler can never accidentally leak them.
type Server struct {
	ID              string          `json:"id"`
	UserID          string          `json:"user_id"`
	Name            string          `json:"name"`
	Token           string          `json:"-"`
	ConnectedAt     string          `json:"connected_at"`
	LastSeen        string          `json:"last_seen"`
	Status          string          `json:"status"`
	AgentID         sql.NullString  `json:"-"`
	AgentSecretHash sql.NullString  `json:"-"`
	OSInfo          sql.NullString  `json:"os_info,omitempty"`
	Arch            sql.NullString  `json:"arch,omitempty"`
	RAMMB           sql.NullInt64   `json:"ram_mb,omitempty"`
	DiskGB          sql.NullInt64   `json:"disk_gb,omitempty"`
	IPAddress       sql.NullString  `json:"ip_address,omitempty"`
	OSVersion       sql.NullString  `json:"os_version,omitempty"`
	OSPretty        sql.NullString  `json:"os_pretty,omitempty"`
	RAMAvailableMB  sql.NullInt64   `json:"ram_available_mb,omitempty"`
	DiskTotalGB     sql.NullInt64   `json:"disk_total_gb,omitempty"`
	DiskAvailableGB sql.NullInt64   `json:"disk_available_gb,omitempty"`
	DiskUsedPercent sql.NullFloat64 `json:"disk_used_percent,omitempty"`
	DockerVersion   sql.NullString  `json:"docker_version,omitempty"`
}

const serverColumns = `id, user_id, name, token, connected_at, last_seen, status,
	agent_id, agent_secret_hash, os_info, arch, ram_mb, disk_gb, ip_address,
	os_version, os_pretty, ram_available_mb, disk_total_gb, disk_available_gb,
	disk_used_percent, docker_version`

func scanServer(scanner interface{ Scan(...any) error }) (Server, error) {
	var s Server
	if err := scanner.Scan(&s.ID, &s.UserID, &s.Name, &s.Token, &s.ConnectedAt, &s.LastSeen, &s.Status,
		&s.AgentID, &s.AgentSecretHash, &s.OSInfo, &s.Arch, &s.RAMMB, &s.DiskGB, &s.IPAddress,
		&s.OSVersion, &s.OSPretty, &s.RAMAvailableMB, &s.DiskTotalGB, &s.DiskAvailableGB,
		&s.DiskUsedPercent, &s.DockerVersion); err != nil {
		return s, err
	}
	return s, nil
}

// GetServer returns a server by ID (Layer 5C Step 3A Pattern 1).
// Returns nil, nil when the server does not exist.
func GetServer(db *sql.DB, serverID string) (*Server, error) {
	s, err := scanServer(db.QueryRow(
		"SELECT "+serverColumns+" FROM servers WHERE id = ?",
		serverID,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListServersByUser returns every server owned by the user (Pattern 3).
// Returns an empty slice (never nil) when the user has no servers.
// The servers table has no created_at column, so ordering uses connected_at.
func ListServersByUser(db *sql.DB, userID string) ([]Server, error) {
	rows, err := db.Query(
		"SELECT "+serverColumns+" FROM servers WHERE user_id = ? ORDER BY connected_at DESC",
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []Server
	for rows.Next() {
		s, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, s)
	}
	if servers == nil {
		servers = []Server{}
	}
	return servers, rows.Err()
}

// ListServersByTeam returns every server linked to a team via server_team
// (Pattern 3). Returns an empty slice (never nil) when the team has none.
func ListServersByTeam(db *sql.DB, teamID string) ([]Server, error) {
	rows, err := db.Query(
		"SELECT "+serverColumns+" FROM servers s "+
			"JOIN server_team st ON st.server_id = s.id "+
			"WHERE st.team_id = ? ORDER BY s.connected_at DESC",
		teamID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []Server
	for rows.Next() {
		s, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, s)
	}
	if servers == nil {
		servers = []Server{}
	}
	return servers, rows.Err()
}
