package queries

import (
	"database/sql"
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
	_, err := db.Exec(
		"INSERT INTO servers (id, user_id, name, agent_id, agent_secret_hash, os_info, arch, ram_mb, disk_gb, ip_address, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'connected')",
		id, userID, name, agentID, agentSecretHash, osInfo, arch, ramMB, diskGB, ipAddress,
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

func InsertServerEvent(db *sql.DB, id, serverID, eventType, checkName, message, details string) error {
	_, err := db.Exec(
		"INSERT INTO server_events (id, server_id, event_type, check_name, message, details) VALUES (?, ?, ?, ?, ?, ?)",
		id, serverID, eventType, checkName, message, details,
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