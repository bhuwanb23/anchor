package queries

import (
	"database/sql"
)

// ContainerStatusRow holds the current (latest) status of a container.
type ContainerStatusRow struct {
	Project      string
	Role         string
	ContainerID  string
	Status       string
	Health       sql.NullString
	CPUPercent   float64
	RAMUsedMB    int64
	RAMLimitMB   int64
	RAMPercent   float64
	RestartCount int
	UptimeSecs   int64
	ExitCode     sql.NullInt64
	NetRxBytes   uint64
	NetTxBytes   uint64
}

// UpsertContainerStatus inserts or replaces the latest container status snapshot.
func UpsertContainerStatus(db *sql.DB, serverID, project, role, containerID, status string, health *string, cpuPct float64, ramUsedMB, ramLimitMB int64, ramPct float64, restartCount int, uptimeSecs int64, exitCode *int, netRx, netTx uint64) error {
	id := serverID + ":" + project + ":" + role
	var healthVal interface{}
	if health != nil {
		healthVal = *health
	}
	var exitCodeVal interface{}
	if exitCode != nil {
		exitCodeVal = *exitCode
	}
	_, err := db.Exec(`
		INSERT INTO container_status (id, server_id, project, role, container_id, status, health,
			cpu_percent, ram_used_mb, ram_limit_mb, ram_percent, restart_count, uptime_secs,
			exit_code, net_rx_bytes, net_tx_bytes, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			container_id=excluded.container_id, status=excluded.status, health=excluded.health,
			cpu_percent=excluded.cpu_percent, ram_used_mb=excluded.ram_used_mb,
			ram_limit_mb=excluded.ram_limit_mb, ram_percent=excluded.ram_percent,
			restart_count=excluded.restart_count, uptime_secs=excluded.uptime_secs,
			exit_code=excluded.exit_code, net_rx_bytes=excluded.net_rx_bytes,
			net_tx_bytes=excluded.net_tx_bytes, last_seen=datetime('now')`,
		id, serverID, project, role, containerID, status, healthVal,
		cpuPct, ramUsedMB, ramLimitMB, ramPct, restartCount, uptimeSecs,
		exitCodeVal, netRx, netTx,
	)
	return err
}

// InsertMetric inserts a raw 30-second health-metrics sample.
func InsertMetric(db *sql.DB, id, serverID, recordedAt string, collectedInMS int64, cpuPct float64, ramUsedMB, ramTotalMB int64, ramPct float64, diskUsedGB, diskTotalGB, diskPct float64, load1, loadPerCore float64, caddyRunning bool, caddyRoutes int, backupAgeSec *int64, containerCount int) error {
	var backupAgeVal interface{}
	if backupAgeSec != nil {
		backupAgeVal = *backupAgeSec
	}
	caddyRunningInt := 0
	if caddyRunning {
		caddyRunningInt = 1
	}
	_, err := db.Exec(`
		INSERT INTO metrics_history (id, server_id, recorded_at, collected_in_ms,
			cpu_percent, ram_used_mb, ram_total_mb, ram_percent,
			disk_used_gb, disk_total_gb, disk_percent,
			load_1min, load_per_core,
			caddy_running, caddy_routes_count, last_backup_age_sec, container_count)
		VALUES (?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?,
			?, ?,
			?, ?, ?, ?)`,
		id, serverID, recordedAt, collectedInMS,
		cpuPct, ramUsedMB, ramTotalMB, ramPct,
		diskUsedGB, diskTotalGB, diskPct,
		load1, loadPerCore,
		caddyRunningInt, caddyRoutes, backupAgeVal, containerCount,
	)
	return err
}

// DeleteMetricsBefore deletes metrics_history rows older than the given cutoff
// for all servers when serverID is "", or for a specific server otherwise.
func DeleteMetricsBefore(db *sql.DB, serverID, cutoff string) error {
	if serverID == "" {
		_, err := db.Exec("DELETE FROM metrics_history WHERE recorded_at < ?", cutoff)
		return err
	}
	_, err := db.Exec("DELETE FROM metrics_history WHERE server_id = ? AND recorded_at < ?", serverID, cutoff)
	return err
}

// GetServerContainers returns all current container status rows for a server.
func GetServerContainers(db *sql.DB, serverID string) ([]ContainerStatusRow, error) {
	rows, err := db.Query(`
		SELECT project, role, container_id, status, health,
			cpu_percent, ram_used_mb, ram_limit_mb, ram_percent,
			restart_count, uptime_secs, exit_code, net_rx_bytes, net_tx_bytes
		FROM container_status WHERE server_id = ?`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ContainerStatusRow
	for rows.Next() {
		var r ContainerStatusRow
		if err := rows.Scan(&r.Project, &r.Role, &r.ContainerID, &r.Status, &r.Health,
			&r.CPUPercent, &r.RAMUsedMB, &r.RAMLimitMB, &r.RAMPercent,
			&r.RestartCount, &r.UptimeSecs, &r.ExitCode, &r.NetRxBytes, &r.NetTxBytes); err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
