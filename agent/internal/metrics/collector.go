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

// BackupStateReader reports the age of the last successful backup.
// Implemented by *state.Manager.
type BackupStateReader interface {
	GetLastBackupTime() time.Time
}

// SystemCollector gathers host-level (server) and platform-level metrics.
type SystemCollector struct {
	caddy  CaddyStatus
	backup BackupStateReader
}

// NewSystemCollector creates a SystemCollector.
func NewSystemCollector(caddy CaddyStatus, backup BackupStateReader) *SystemCollector {
	return &SystemCollector{
		caddy:  caddy,
		backup: backup,
	}
}

// Collect gathers server and platform metrics. Container metrics are handled
// separately by the DockerCollector.
func (c *SystemCollector) Collect(ctx context.Context) (ServerMetrics, PlatformMetrics) {
	return c.collectServer(ctx), c.collectPlatform()
}

func (c *SystemCollector) collectServer(ctx context.Context) ServerMetrics {
	var srv ServerMetrics

	// CPU percent since the previous sample. gopsutil computes the delta
	// internally when passed interval=0; the first call returns the
	// since-boot average, which is a fine baseline.
	if percents, err := cpu.Percent(0, false); err == nil && len(percents) > 0 {
		srv.CPUPercent = percents[0]
	} else if err != nil {
		slog.Warn("metrics: cpu read failed", "error", err)
	}

	srv.CPUCores = cpuCount()

	// Load average (1-minute) and per-core value.
	if l, err := load.Avg(); err == nil {
		srv.Load1Min = l.Load1
		if srv.CPUCores > 0 {
			srv.LoadPerCore = l.Load1 / float64(srv.CPUCores)
		}
	} else {
		slog.Warn("metrics: load read failed", "error", err)
	}

	// Memory — use Available (reclaimable cache counts toward available).
	if v, err := mem.VirtualMemory(); err == nil {
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
	collectDisk("/", &srv.DiskUsedGB, &srv.DiskTotalGB, &srv.DiskPercent)
	if isSeparateMount("/var") {
		collectDisk("/var", &srv.VarDiskUsedGB, &srv.VarDiskTotalGB, &srv.VarDiskPercent)
	}

	// Network — cumulative bytes across interfaces (excluding loopback).
	if rx, tx, ok := networkDelta(); ok {
		srv.NetRxBytes = rx
		srv.NetTxBytes = tx
	}

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
		if t := c.backup.GetLastBackupTime(); !t.IsZero() {
			plat.LastBackupAt = t.UTC().Format(time.RFC3339)
			plat.LastBackupAgeSec = int64(time.Since(t).Seconds())
		}
	}

	return plat
}

func cpuCount() int {
	n, err := cpu.Counts(true)
	if err != nil || n <= 0 {
		return 1
	}
	return n
}

func collectDisk(path string, usedGB, totalGB *float64, percent *float64) {
	u, err := disk.Usage(path)
	if err != nil {
		slog.Warn("metrics: disk read failed", "path", path, "error", err)
		return
	}
	*totalGB = float64(u.Total) / 1024 / 1024 / 1024
	*usedGB = float64(u.Total-u.Free) / 1024 / 1024 / 1024
	*percent = u.UsedPercent
}

// isSeparateMount reports whether path is a distinct, real filesystem from "/".
func isSeparateMount(path string) bool {
	partitions, err := disk.Partitions(false)
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

// networkDelta returns aggregate bytes-in / bytes-out across all interfaces
// except the loopback device.
func networkDelta() (uint64, uint64, bool) {
	ioStats, err := net.IOCounters(true)
	if err != nil {
		slog.Warn("metrics: network read failed", "error", err)
		return 0, 0, false
	}
	var rx, tx uint64
	for _, s := range ioStats {
		if s.Name == "lo" {
			continue
		}
		rx += s.BytesRecv
		tx += s.BytesSent
	}
	return rx, tx, true
}
