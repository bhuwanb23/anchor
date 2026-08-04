package metrics

import "time"

// ContainerMetrics holds a single container's health and resource usage
// as sampled during one collection cycle.
type ContainerMetrics struct {
	Project      string  `json:"project"`
	Role         string  `json:"role"`
	ContainerID  string  `json:"container_id"`
	Status       string  `json:"status"` // running, exited, paused, restarting, created, dead
	Health       *string `json:"health"` // healthy, unhealthy, starting, unknown; null when N/A
	CPUPercent   float64 `json:"cpu_percent"`
	RAMUsedMB    int64   `json:"ram_used_mb"`
	RAMLimitMB   int64   `json:"ram_limit_mb"`
	RAMPercent   float64 `json:"ram_percent"`
	NetRxBytes   uint64  `json:"net_rx_bytes"`
	NetTxBytes   uint64  `json:"net_tx_bytes"`
	RestartCount int     `json:"restart_count"`
	UptimeSecs   int64   `json:"uptime_seconds"`
	ExitCode     *int    `json:"exit_code"` // null when the container is running
	// OOMKilled is true when the container was (or is) OOM-killed by the
	// kernel (inspect.State.OOMKilled); stays true until the container is
	// recreated. Drives the container_oom anomaly alert.
	OOMKilled bool `json:"oom_killed"`
}

// ServerMetrics holds host-level resource usage from one collection cycle.
type ServerMetrics struct {
	CPUPercent     float64 `json:"cpu_percent"`
	RAMUsedMB      int64   `json:"ram_used_mb"`
	RAMTotalMB     int64   `json:"ram_total_mb"`
	RAMPercent     float64 `json:"ram_percent"`
	DiskUsedGB     float64 `json:"disk_used_gb"`
	DiskTotalGB    float64 `json:"disk_total_gb"`
	DiskPercent    float64 `json:"disk_percent"`
	VarDiskUsedGB  float64 `json:"var_disk_used_gb,omitempty"`
	VarDiskTotalGB float64 `json:"var_disk_total_gb,omitempty"`
	VarDiskPercent float64 `json:"var_disk_percent,omitempty"`
	Load1Min       float64 `json:"load_1min"`
	LoadPerCore    float64 `json:"load_per_core"`
	CPUCores       int     `json:"cpu_cores"`
	NetRxBytes     uint64  `json:"net_rx_bytes"`
	NetTxBytes     uint64  `json:"net_tx_bytes"`

	// NetRxBytesPerSec / NetTxBytesPerSec are per-interval (30s) transfer
	// rates derived from the cumulative counters. They are omitted on the
	// first cycle, which has no previous sample to compute a delta from.
	NetRxBytesPerSec uint64 `json:"net_rx_bytes_per_sec,omitempty"`
	NetTxBytesPerSec uint64 `json:"net_tx_bytes_per_sec,omitempty"`
}

// PlatformMetrics captures the health of the platform's own components.
type PlatformMetrics struct {
	CaddyRunning     bool   `json:"caddy_running"`
	CaddyRoutesCount int    `json:"caddy_routes_count"`
	LastBackupAt     string `json:"last_backup_at,omitempty"`     // RFC3339, empty if never
	LastBackupAgeSec int64  `json:"last_backup_age_seconds"`      // 0 if never
	LastBackupStatus string `json:"last_backup_status,omitempty"` // "success", empty if never
	AgentVersion     string `json:"agent_version,omitempty"`
	AgentUptimeSec   int64  `json:"agent_uptime_seconds"`
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
