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
	if out == nil {
		out = []ContainerStatusRow{}
	}
	return out, rows.Err()
}

// MetricRow is one metrics_history sample with nullable fields (older rows or
// partial reports may leave columns NULL).
type MetricRow struct {
	ID               string
	ServerID         string
	RecordedAt       string
	CollectedInMS    *int64
	CPUPercent       *float64
	RAMUsedMB        *int64
	RAMTotalMB       *int64
	RAMPercent       *float64
	DiskUsedGB       *float64
	DiskTotalGB      *float64
	DiskPercent      *float64
	Load1Min         *float64
	LoadPerCore      *float64
	CaddyRunning     *bool
	CaddyRoutesCount *int
	LastBackupAgeSec *int64
	ContainerCount   *int
}

// GetLatestMetric returns the most recent health metrics sample for a server,
// or nil if the server has no samples yet. Used for the server_state snapshot
// a browser receives on subscribe (Layer 5B Step 3B).
func GetLatestMetric(db *sql.DB, serverID string) (*MetricRow, error) {
	row := db.QueryRow(`
		SELECT id, server_id, recorded_at, collected_in_ms,
		       cpu_percent, ram_used_mb, ram_total_mb, ram_percent,
		       disk_used_gb, disk_total_gb, disk_percent,
		       load_1min, load_per_core,
		       caddy_running, caddy_routes_count, last_backup_age_sec, container_count
		FROM metrics_history
		WHERE server_id = ?
		ORDER BY recorded_at DESC
		LIMIT 1`, serverID)

	var m MetricRow
	var collected, ramUsed, ramTotal, backupAge sql.NullInt64
	var cpu, ramPct, diskUsed, diskTotal, diskPct, load1, loadPerCore sql.NullFloat64
	var caddyRunning, caddyRoutes, containerCount sql.NullInt64
	if err := row.Scan(&m.ID, &m.ServerID, &m.RecordedAt, &collected,
		&cpu, &ramUsed, &ramTotal, &ramPct,
		&diskUsed, &diskTotal, &diskPct,
		&load1, &loadPerCore,
		&caddyRunning, &caddyRoutes, &backupAge, &containerCount); err != nil {
		return nil, err
	}
	m.CollectedInMS = nullInt64Ptr(collected)
	m.CPUPercent = nullFloat64Ptr(cpu)
	m.RAMUsedMB = nullInt64Ptr(ramUsed)
	m.RAMTotalMB = nullInt64Ptr(ramTotal)
	m.RAMPercent = nullFloat64Ptr(ramPct)
	m.DiskUsedGB = nullFloat64Ptr(diskUsed)
	m.DiskTotalGB = nullFloat64Ptr(diskTotal)
	m.DiskPercent = nullFloat64Ptr(diskPct)
	m.Load1Min = nullFloat64Ptr(load1)
	m.LoadPerCore = nullFloat64Ptr(loadPerCore)
	if caddyRunning.Valid {
		running := caddyRunning.Int64 != 0
		m.CaddyRunning = &running
	}
	if caddyRoutes.Valid {
		r := int(caddyRoutes.Int64)
		m.CaddyRoutesCount = &r
	}
	m.LastBackupAgeSec = nullInt64Ptr(backupAge)
	if containerCount.Valid {
		c := int(containerCount.Int64)
		m.ContainerCount = &c
	}
	return &m, nil
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func nullFloat64Ptr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	out := v.Float64
	return &out
}

// UpsertServerMetricsLatest updates the latest metrics snapshot for a server.
// Called on every health report for O(1) dashboard load.
func UpsertServerMetricsLatest(db *sql.DB, serverID, recordedAt string, cpuPct float64, ramUsedMB, ramTotalMB int64, diskUsedGB, diskTotalGB, load1 float64) error {
	_, err := db.Exec(`
		INSERT INTO server_metrics_latest (server_id, recorded_at, cpu_percent, ram_used_mb, ram_total_mb, disk_used_gb, disk_total_gb, load_1min)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(server_id) DO UPDATE SET
			recorded_at = excluded.recorded_at,
			cpu_percent = excluded.cpu_percent,
			ram_used_mb = excluded.ram_used_mb,
			ram_total_mb = excluded.ram_total_mb,
			disk_used_gb = excluded.disk_used_gb,
			disk_total_gb = excluded.disk_total_gb,
			load_1min = excluded.load_1min`,
		serverID, recordedAt, cpuPct, ramUsedMB, ramTotalMB, diskUsedGB, diskTotalGB, load1,
	)
	return err
}

// GetServerMetricsLatest returns the latest metrics snapshot for a server.
func GetServerMetricsLatest(db *sql.DB, serverID string) (*MetricRow, error) {
	row := db.QueryRow(`
		SELECT server_id, recorded_at, cpu_percent, ram_used_mb, ram_total_mb, disk_used_gb, disk_total_gb, load_1min
		FROM server_metrics_latest WHERE server_id = ?`, serverID)

	var m MetricRow
	var cpu, diskUsed, diskTotal, load1 sql.NullFloat64
	var ramUsed, ramTotal sql.NullInt64
	if err := row.Scan(&m.ServerID, &m.RecordedAt, &cpu, &ramUsed, &ramTotal, &diskUsed, &diskTotal, &load1); err != nil {
		return nil, err
	}
	m.CPUPercent = nullFloat64Ptr(cpu)
	m.RAMUsedMB = nullInt64Ptr(ramUsed)
	m.RAMTotalMB = nullInt64Ptr(ramTotal)
	m.DiskUsedGB = nullFloat64Ptr(diskUsed)
	m.DiskTotalGB = nullFloat64Ptr(diskTotal)
	m.Load1Min = nullFloat64Ptr(load1)
	return &m, nil
}

// RollupHourlyMetrics aggregates raw metrics into hourly averages.
// Returns the number of hourly rows inserted.
func RollupHourlyMetrics(db *sql.DB) (int64, error) {
	result, err := db.Exec(`
		INSERT OR IGNORE INTO metrics_history (id, server_id, recorded_at, cpu_percent, ram_used_mb, ram_total_mb, disk_used_gb, disk_total_gb, load_1min)
		SELECT
			lower(hex(randomblob(16))),
			server_id,
			strftime('%Y-%m-%dT%H:00:00Z', recorded_at) as hour,
			AVG(cpu_percent),
			AVG(ram_used_mb),
			AVG(ram_total_mb),
			AVG(disk_used_gb),
			AVG(disk_total_gb),
			AVG(load_1min)
		FROM metrics_history
		WHERE recorded_at < datetime('now', '-1 hour')
		  AND recorded_at >= datetime('now', '-8 days')
		GROUP BY server_id, hour`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DeleteOldRawMetrics removes raw metrics older than 7 days.
func DeleteOldRawMetrics(db *sql.DB) (int64, error) {
	result, err := db.Exec(`
		DELETE FROM metrics_history
		WHERE recorded_at < datetime('now', '-7 days')`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DeleteOldMetrics removes metrics older than the given number of days.
func DeleteOldMetrics(db *sql.DB, days int) (int64, error) {
	result, err := db.Exec(
		`DELETE FROM metrics_history WHERE recorded_at < datetime('now', '-' || ? || ' days')`,
		days,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
