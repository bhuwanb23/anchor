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