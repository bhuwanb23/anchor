package metrics

import "time"

// ContainerMetrics holds a single container's health and resource usage
// as sampled during one collection cycle.
type ContainerMetrics struct {
	Project      string  `json:"project"`
	Role         string  `json:"role"`
	ContainerID  string  `json:"container_id"`
	Status       string  `json:"status"` // running, exited, paused, restarting, created, dead
	Health       string  `json:"health"` // healthy, unhealthy, starting, unknown
	CPUPercent   float64 `json:"cpu_percent"`
	RAMUsedMB    int64   `json:"ram_used_mb"`
	RAMLimitMB   int64   `json:"ram_limit_mb"`
	RAMPercent   float64 `json:"ram_percent"`
	NetRxBytes   uint64  `json:"net_rx_bytes"`
	NetTxBytes   uint64  `json:"net_tx_bytes"`
	RestartCount int     `json:"restart_count"`
	UptimeSecs   int64   `json:"uptime_seconds"`
	ExitCode     *int    `json:"exit_code,omitempty"`
}

// ServerMetrics holds host-level resource usage from one collection cycle.
type ServerMetrics struct {
	CPUPercent float64 `json:"cpu_percent"`
	RAMUsedMB  int64   `json:"ram_used_mb"`
	RAMTotalMB int64   `json:"ram_total_mb"`
	RAMPercent float64 `json:"ram_percent"`
	DiskUsedGB float64 `json:"disk_used_gb"`
	DiskTotalGB float64 `json:"disk_total_gb"`
	DiskPercent float64 `json:"disk_percent"`
	VarDiskUsedGB float64 `json:"var_disk_used_gb,omitempty"`
	VarDiskTotalGB float64 `json:"var_disk_total_gb,omitempty"`
	VarDiskPercent float64 `json:"var_disk_percent,omitempty"`
	Load1Min    float64 `json:"load_1min"`
	LoadPerCore float64 `json:"load_per_core"`
	CPUCores    int     `json:"cpu_cores"`
	NetRxBytes  uint64  `json:"net_rx_bytes"`
	NetTxBytes  uint64  `json:"net_tx_bytes"`
}

// PlatformMetrics captures the health of the platform's own components.
type PlatformMetrics struct {
	CaddyRunning     bool   `json:"caddy_running"`
	CaddyRoutesCount int    `json:"caddy_routes_count"`
	LastBackupAt     string `json:"last_backup_at,omitempty"` // RFC3339, empty if never
	LastBackupAgeSec int64  `json:"last_backup_age_seconds"`
}

// HealthReport is the full payload sent to the control plane each cycle.
type HealthReport struct {
	Type          string             `json:"type"` // "health_report"
	ServerID      string             `json:"server_id"`
	Timestamp     time.Time          `json:"timestamp"`
	CollectedInMS int64              `json:"collected_in_ms"`
	Server        ServerMetrics      `json:"server"`
	Containers    []ContainerMetrics `json:"containers"`
	Platform      PlatformMetrics    `json:"platform"`
}
