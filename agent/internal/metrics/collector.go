package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// CaddyStatus provides platform metrics about the reverse proxy.
// Implemented indirectly via an adapter (see main.go) because it must not
// depend on caddy types.
type CaddyStatus interface {
	IsAlive() bool
	RoutesCount() (int, error)
}

// BackupStateReader reports the age and status of the last completed backup.
// Implemented by *state.Manager.
type BackupStateReader interface {
	GetLastBackupTime() time.Time
	GetLastBackupStatus() string
}

// SystemCollector gathers host-level (server) and platform-level metrics.
type SystemCollector struct {
	caddy  CaddyStatus
	backup BackupStateReader

	// lastNetRx / lastNetTx / lastNetAt hold the previous cumulative network
	// counters and the time they were sampled, used to derive per-interval
	// transfer rates on the next cycle. Collect is only ever called from the
	// Manager's single collection goroutine, so no synchronization is needed.
	lastNetRx uint64
	lastNetTx uint64
	lastNetAt time.Time

	// agentVersion and agentStartedAt populate the platform block's
	// agent_version / agent_uptime_seconds fields. Set via WithAgentInfo.
	agentVersion   string
	agentStartedAt time.Time

	// Narrow seams over gopsutil so the collector is unit-testable without
	// touching the host. Defaults point at the real gopsutil functions and
	// are overridden only in tests.
	cpuPercent     func(interval time.Duration, perCPU bool) ([]float64, error)
	cpuCounts      func(logical bool) (int, error)
	loadAvg        func() (*load.AvgStat, error)
	virtualMem     func() (*mem.VirtualMemoryStat, error)
	diskUsage      func(path string) (*disk.UsageStat, error)
	diskPartitions func(all bool) ([]disk.PartitionStat, error)
	netCounters    func(perProc bool) ([]net.IOCountersStat, error)
	now            func() time.Time
}

// NewSystemCollector creates a SystemCollector.
func NewSystemCollector(caddy CaddyStatus, backup BackupStateReader) *SystemCollector {
	return &SystemCollector{
		caddy:          caddy,
		backup:         backup,
		cpuPercent:     cpu.Percent,
		cpuCounts:      cpu.Counts,
		loadAvg:        load.Avg,
		virtualMem:     mem.VirtualMemory,
		diskUsage:      disk.Usage,
		diskPartitions: disk.Partitions,
		netCounters:    net.IOCounters,
		now:            time.Now,
	}
}

// WithAgentInfo sets the agent version and process start time so the platform
// block can report agent_version and agent_uptime_seconds.
func (c *SystemCollector) WithAgentInfo(version string, startedAt time.Time) *SystemCollector {
	c.agentVersion = version
	if !startedAt.IsZero() {
		c.agentStartedAt = startedAt
	}
	return c
}

// Collect gathers server and platform metrics. Container metrics are handled
// separately by the DockerCollector.
func (c *SystemCollector) Collect(ctx context.Context) (ServerMetrics, PlatformMetrics) {
	return c.collectServer(ctx), c.collectPlatform()
}

func (c *SystemCollector) collectServer(ctx context.Context) ServerMetrics {
	var srv ServerMetrics

	// CPU percent is the delta over the interval since the previous call
	// (30s in production). gopsutil implements the same math as the Layer 4C
	// plan's /proc/stat method: it reads the cumulative counters twice and
	// computes (used_delta / total_delta) * 100. The very first call has no
	// prior sample, so it returns the since-boot average as a baseline; from
	// the second tick onward the value is a true interval sample.
	if percents, err := c.cpuPercent(0, false); err == nil && len(percents) > 0 {
		srv.CPUPercent = percents[0]
	} else if err != nil {
		slog.Warn("metrics: cpu read failed", "error", err)
	}

	srv.CPUCores = c.cpuCount()

	// Load average (1-minute) and per-core value.
	if l, err := c.loadAvg(); err == nil {
		srv.Load1Min = l.Load1
		if srv.CPUCores > 0 {
			srv.LoadPerCore = l.Load1 / float64(srv.CPUCores)
		}
	} else {
		slog.Warn("metrics: load read failed", "error", err)
	}

	// Memory — use MemAvailable (reclaimable cache counts toward available),
	// never MemFree, exactly as the Layer 4C plan specifies.
	if v, err := c.virtualMem(); err == nil {
		totalMB := int64(v.Total / 1024 / 1024)
		availMB := int64(v.Available / 1024 / 1024)
		srv.RAMTotalMB = totalMB
		srv.RAMUsedMB = totalMB - availMB
		if srv.RAMTotalMB > 0 {
			srv.RAMPercent = float64(srv.RAMUsedMB) / float64(srv.RAMTotalMB) * 100
		}
	} else {
		slog.Warn("metrics: mem read failed", "error", err)
	}

	// Disk — root always, /var only if it is a separate mount.
	c.collectDisk("/", &srv.DiskUsedGB, &srv.DiskTotalGB, &srv.DiskPercent)
	if c.isSeparateMount("/var") {
		c.collectDisk("/var", &srv.VarDiskUsedGB, &srv.VarDiskTotalGB, &srv.VarDiskPercent)
	}

	// Network — cumulative bytes across interfaces plus per-interval rates.
	c.collectNetwork(&srv)

	return srv
}

func (c *SystemCollector) collectPlatform() PlatformMetrics {
	plat := PlatformMetrics{}

	if c.caddy != nil {
		plat.CaddyRunning = c.caddy.IsAlive()
		if n, err := c.caddy.RoutesCount(); err == nil {
			plat.CaddyRoutesCount = n
		} else {
			slog.Warn("metrics: caddy routes read failed", "error", err)
		}
	}

	if c.backup != nil {
		plat.LastBackupStatus = c.backup.GetLastBackupStatus()
		if t := c.backup.GetLastBackupTime(); !t.IsZero() {
			plat.LastBackupAt = t.UTC().Format(time.RFC3339)
			plat.LastBackupAgeSec = int64(time.Since(t).Seconds())
		}
	}

	plat.AgentVersion = c.agentVersion
	if !c.agentStartedAt.IsZero() {
		plat.AgentUptimeSec = int64(time.Since(c.agentStartedAt).Seconds())
	}

	return plat
}

func (c *SystemCollector) cpuCount() int {
	n, err := c.cpuCounts(true)
	if err != nil || n <= 0 {
		return 1
	}
	return n
}

func (c *SystemCollector) collectDisk(path string, usedGB, totalGB *float64, percent *float64) {
	u, err := c.diskUsage(path)
	if err != nil {
		slog.Warn("metrics: disk read failed", "path", path, "error", err)
		return
	}
	*totalGB = float64(u.Total) / 1024 / 1024 / 1024
	*usedGB = float64(u.Total-u.Free) / 1024 / 1024 / 1024
	*percent = u.UsedPercent
}

// isSeparateMount reports whether path is a distinct, real filesystem from "/".
func (c *SystemCollector) isSeparateMount(path string) bool {
	partitions, err := c.diskPartitions(false)
	if err != nil {
		return false
	}
	for _, p := range partitions {
		if p.Mountpoint == path && p.Device != "" && p.Device != "overlay" {
			return true
		}
	}
	return false
}

// collectNetwork reads aggregate bytes-in / bytes-out across all interfaces
// except loopback and derives per-interval rates from the previous sample.
// On the first cycle there is no baseline, so only the cumulative totals are
// reported and the rates stay zero (omitted from the wire format).
func (c *SystemCollector) collectNetwork(srv *ServerMetrics) {
	ioStats, err := c.netCounters(true)
	if err != nil {
		slog.Warn("metrics: network read failed", "error", err)
		return
	}

	var rx, tx uint64
	for _, s := range ioStats {
		if s.Name == "lo" {
			continue
		}
		rx += s.BytesRecv
		tx += s.BytesSent
	}

	now := c.now()
	if !c.lastNetAt.IsZero() {
		elapsed := now.Sub(c.lastNetAt).Seconds()
		if elapsed > 0 {
			srv.NetRxBytesPerSec = networkRate(rx, c.lastNetRx, elapsed)
			srv.NetTxBytesPerSec = networkRate(tx, c.lastNetTx, elapsed)
		}
	} else {
		slog.Debug("metrics: first network sample, per-interval rates unavailable")
	}

	srv.NetRxBytes = rx
	srv.NetTxBytes = tx
	c.lastNetRx = rx
	c.lastNetTx = tx
	c.lastNetAt = now
}

// networkRate converts a counter delta into a per-second rate, treating a
// counter reset (interface restart / reboot) as a fresh delta.
func networkRate(current, previous uint64, elapsedSec float64) uint64 {
	var delta uint64
	if current >= previous {
		delta = current - previous
	} else {
		delta = current // counter reset since the last sample
	}
	return uint64(float64(delta) / elapsedSec)
}
