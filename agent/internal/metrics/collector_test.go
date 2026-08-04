package metrics

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// stubSeams replaces every gopsutil seam on c with deterministic fixtures.
// Individual tests override the fields they care about after calling it.
func stubSeams(c *SystemCollector) *SystemCollector {
	c.cpuPercent = func(time.Duration, bool) ([]float64, error) { return []float64{10}, nil }
	c.cpuCounts = func(bool) (int, error) { return 4, nil }
	c.loadAvg = func() (*load.AvgStat, error) { return &load.AvgStat{Load1: 1.0}, nil }
	c.virtualMem = func() (*mem.VirtualMemoryStat, error) {
		return &mem.VirtualMemoryStat{Total: 4096 << 20, Free: 1024 << 20, Available: 2048 << 20}, nil
	}
	c.diskUsage = func(path string) (*disk.UsageStat, error) {
		return &disk.UsageStat{Total: 200 << 30, Free: 100 << 30, UsedPercent: 50}, nil
	}
	c.diskPartitions = func(bool) ([]disk.PartitionStat, error) { return nil, nil }
	c.netCounters = func(bool) ([]net.IOCountersStat, error) { return nil, nil }
	c.now = time.Now
	return c
}

// testCollector returns a SystemCollector with every gopsutil seam stubbed to
// a small, deterministic fixture.
func testCollector() *SystemCollector {
	return stubSeams(NewSystemCollector(nil, nil))
}

func TestSystemCollector_RAMUsesMemAvailable(t *testing.T) {
	// Total 4096MB, Free 1024MB, but Available 2048MB. Using MemFree would
	// report 3072MB used (75%); MemAvailable must yield 2048MB used (50%).
	c := testCollector()
	c.virtualMem = func() (*mem.VirtualMemoryStat, error) {
		return &mem.VirtualMemoryStat{
			Total:     4096 << 20,
			Free:      1024 << 20,
			Available: 2048 << 20,
		}, nil
	}

	srv, _ := c.Collect(context.Background())

	if srv.RAMTotalMB != 4096 {
		t.Errorf("RAMTotalMB = %d, want 4096", srv.RAMTotalMB)
	}
	if srv.RAMUsedMB != 2048 {
		t.Errorf("RAMUsedMB = %d, want 2048 (total-available), not 3072 (total-free)",
			srv.RAMUsedMB)
	}
	if srv.RAMPercent < 49.9 || srv.RAMPercent > 50.1 {
		t.Errorf("RAMPercent = %f, want ~50", srv.RAMPercent)
	}
}

func TestSystemCollector_LoadPerCore(t *testing.T) {
	c := testCollector()
	c.cpuCounts = func(bool) (int, error) { return 8, nil }
	c.loadAvg = func() (*load.AvgStat, error) { return &load.AvgStat{Load1: 2.0}, nil }

	srv, _ := c.Collect(context.Background())

	if srv.CPUCores != 8 {
		t.Errorf("CPUCores = %d, want 8", srv.CPUCores)
	}
	if srv.Load1Min != 2.0 {
		t.Errorf("Load1Min = %f, want 2.0", srv.Load1Min)
	}
	if srv.LoadPerCore != 0.25 {
		t.Errorf("LoadPerCore = %f, want 0.25", srv.LoadPerCore)
	}
}

func TestSystemCollector_SeparateVarMount(t *testing.T) {
	c := testCollector()
	c.diskPartitions = func(bool) ([]disk.PartitionStat, error) {
		return []disk.PartitionStat{
			{Mountpoint: "/", Device: "/dev/sda1"},
			{Mountpoint: "/var", Device: "/dev/sda2"},
		}, nil
	}
	c.diskUsage = func(path string) (*disk.UsageStat, error) {
		if path == "/var" {
			return &disk.UsageStat{Total: 100 << 30, Free: 40 << 30, UsedPercent: 60}, nil
		}
		return &disk.UsageStat{Total: 200 << 30, Free: 100 << 30, UsedPercent: 50}, nil
	}

	srv, _ := c.Collect(context.Background())

	if srv.DiskTotalGB < 199 || srv.DiskTotalGB > 201 {
		t.Errorf("DiskTotalGB = %f, want ~200", srv.DiskTotalGB)
	}
	if srv.VarDiskTotalGB < 99 || srv.VarDiskTotalGB > 101 {
		t.Errorf("VarDiskTotalGB = %f, want ~100", srv.VarDiskTotalGB)
	}
	if srv.VarDiskUsedGB < 59 || srv.VarDiskUsedGB > 61 {
		t.Errorf("VarDiskUsedGB = %f, want ~60", srv.VarDiskUsedGB)
	}
	if srv.VarDiskPercent != 60 {
		t.Errorf("VarDiskPercent = %f, want 60", srv.VarDiskPercent)
	}
}

func TestSystemCollector_NoSeparateVarMount(t *testing.T) {
	c := testCollector()
	c.diskPartitions = func(bool) ([]disk.PartitionStat, error) {
		return []disk.PartitionStat{{Mountpoint: "/", Device: "/dev/sda1"}}, nil
	}

	srv, _ := c.Collect(context.Background())

	if srv.VarDiskUsedGB != 0 || srv.VarDiskTotalGB != 0 || srv.VarDiskPercent != 0 {
		t.Errorf("expected zero /var metrics when not a separate mount, got used=%f total=%f pct=%f",
			srv.VarDiskUsedGB, srv.VarDiskTotalGB, srv.VarDiskPercent)
	}
}

func TestSystemCollector_NetworkRates(t *testing.T) {
	base := time.Unix(1000, 0)

	// One fixture per collection cycle: the cumulative counters read that
	// cycle plus the timestamp at which they were sampled. Each seam consumes
	// its own sequence in order, so the test does not depend on the internal
	// call order within collectNetwork.
	type sample struct {
		rx, tx uint64
		now    time.Time
	}
	samples := []sample{
		{rx: 1000, tx: 2000, now: base},
		{rx: 7000, tx: 8000, now: base.Add(30 * time.Second)},
		{rx: 500, tx: 600, now: base.Add(60 * time.Second)}, // counter reset
	}

	var netCall, nowCall int
	c := testCollector()
	c.netCounters = func(bool) ([]net.IOCountersStat, error) {
		s := samples[netCall]
		netCall++
		return []net.IOCountersStat{{Name: "eth0", BytesRecv: s.rx, BytesSent: s.tx}}, nil
	}
	c.now = func() time.Time {
		s := samples[nowCall]
		nowCall++
		return s.now
	}

	ctx := context.Background()

	// Cycle 1: no baseline, cumulative only, rates zero.
	srv1, _ := c.Collect(ctx)
	if srv1.NetRxBytes != 1000 || srv1.NetTxBytes != 2000 {
		t.Errorf("cycle 1 cumulative = %d/%d, want 1000/2000", srv1.NetRxBytes, srv1.NetTxBytes)
	}
	if srv1.NetRxBytesPerSec != 0 || srv1.NetTxBytesPerSec != 0 {
		t.Errorf("cycle 1 rates = %d/%d, want 0/0 (no baseline)",
			srv1.NetRxBytesPerSec, srv1.NetTxBytesPerSec)
	}

	// Cycle 2: 6000 bytes over 30s → 200 B/s each way.
	srv2, _ := c.Collect(ctx)
	if srv2.NetRxBytesPerSec != 200 || srv2.NetTxBytesPerSec != 200 {
		t.Errorf("cycle 2 rates = %d/%d, want 200/200",
			srv2.NetRxBytesPerSec, srv2.NetTxBytesPerSec)
	}
	if srv2.NetRxBytes != 7000 {
		t.Errorf("cycle 2 cumulative rx = %d, want 7000", srv2.NetRxBytes)
	}

	// Cycle 3: counter reset handled — rate uses the new counter as the delta.
	srv3, _ := c.Collect(ctx)
	if srv3.NetRxBytesPerSec != 16 {
		t.Errorf("cycle 3 rx rate = %d, want 16 (500/30 after reset)", srv3.NetRxBytesPerSec)
	}
	if srv3.NetRxBytes != 500 {
		t.Errorf("cycle 3 cumulative rx = %d, want 500", srv3.NetRxBytes)
	}
}

// --- Platform block (Step 2A) ---

type fakeCaddyStatus struct {
	alive  bool
	routes int
}

func (f fakeCaddyStatus) IsAlive() bool             { return f.alive }
func (f fakeCaddyStatus) RoutesCount() (int, error) { return f.routes, nil }

type fakeBackupReader struct {
	lastBackup time.Time
	status     string
}

func (f fakeBackupReader) GetLastBackupTime() time.Time { return f.lastBackup }
func (f fakeBackupReader) GetLastBackupStatus() string  { return f.status }

func TestSystemCollector_PlatformBlock(t *testing.T) {
	startedAt := time.Now().Add(-2 * time.Hour)
	lastBackup := time.Now().Add(-12 * time.Hour).UTC()

	c := stubSeams(NewSystemCollector(
		fakeCaddyStatus{alive: true, routes: 2},
		fakeBackupReader{lastBackup: lastBackup, status: "success"},
	)).WithAgentInfo("1.0.0", startedAt)

	_, plat := c.Collect(context.Background())

	if !plat.CaddyRunning {
		t.Error("caddy_running = false, want true")
	}
	if plat.CaddyRoutesCount != 2 {
		t.Errorf("caddy_routes_count = %d, want 2", plat.CaddyRoutesCount)
	}
	if plat.LastBackupStatus != "success" {
		t.Errorf("last_backup_status = %q, want success", plat.LastBackupStatus)
	}
	if plat.LastBackupAt == "" {
		t.Error("last_backup_at is empty, want RFC3339")
	}
	if plat.LastBackupAgeSec < 12*3600-60 || plat.LastBackupAgeSec > 12*3600+60 {
		t.Errorf("last_backup_age_seconds = %d, want ~43200", plat.LastBackupAgeSec)
	}
	if plat.AgentVersion != "1.0.0" {
		t.Errorf("agent_version = %q, want 1.0.0", plat.AgentVersion)
	}
	if plat.AgentUptimeSec < 2*3600-60 || plat.AgentUptimeSec > 2*3600+60 {
		t.Errorf("agent_uptime_seconds = %d, want ~7200", plat.AgentUptimeSec)
	}
}

// TestHealthReportJSONShape pins the wire format to the Step 2A spec: every
// field named in the plan must be present in the marshaled report, and
// health/exit_code are null when not applicable.
func TestHealthReportJSONShape(t *testing.T) {
	health := "healthy"
	exit := 1
	rep := HealthReport{
		Type:          "health_report",
		ServerID:      "srv-test",
		Timestamp:     time.Unix(1700000000, 0).UTC(),
		CollectedInMS: 342,
		Server: ServerMetrics{
			CPUPercent: 45.2, RAMUsedMB: 1228, RAMTotalMB: 2048, RAMPercent: 59.9,
			DiskUsedGB: 18.4, DiskTotalGB: 40.0, DiskPercent: 46.0,
			Load1Min: 0.82, LoadPerCore: 0.41,
		},
		Containers: []ContainerMetrics{
			{Project: "myshop", Role: "app", ContainerID: "abc123", Status: "running",
				Health: &health, CPUPercent: 12.3, RAMUsedMB: 187, RAMLimitMB: 512,
				RAMPercent: 36.5, RestartCount: 0, UptimeSecs: 86400},
			{Project: "myblog", Role: "app", ContainerID: "ghi789", Status: "exited",
				CPUPercent: 0, RAMUsedMB: 0, RAMLimitMB: 512, RAMPercent: 0,
				RestartCount: 3, UptimeSecs: 0, ExitCode: &exit},
		},
		Platform: PlatformMetrics{
			CaddyRunning: true, CaddyRoutesCount: 2,
			LastBackupAt: "2024-01-15T02:04:23Z", LastBackupStatus: "success",
			AgentVersion: "1.0.0", AgentUptimeSec: 172800,
		},
	}

	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, k := range []string{"type", "server_id", "timestamp", "collected_in_ms", "server", "containers", "platform"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing top-level field %q", k)
		}
	}
	if m["type"] != "health_report" {
		t.Errorf("type = %v, want health_report", m["type"])
	}

	server, _ := m["server"].(map[string]interface{})
	for _, k := range []string{"cpu_percent", "ram_used_mb", "ram_total_mb", "ram_percent",
		"disk_used_gb", "disk_total_gb", "disk_percent", "load_1min", "load_per_core"} {
		if _, ok := server[k]; !ok {
			t.Errorf("server missing field %q", k)
		}
	}

	containers, _ := m["containers"].([]interface{})
	if len(containers) != 2 {
		t.Fatalf("containers length = %d, want 2", len(containers))
	}
	c0 := containers[0].(map[string]interface{})
	for _, k := range []string{"project", "role", "container_id", "status", "health",
		"cpu_percent", "ram_used_mb", "ram_limit_mb", "ram_percent",
		"restart_count", "uptime_seconds", "exit_code"} {
		if _, ok := c0[k]; !ok {
			t.Errorf("container[0] missing field %q", k)
		}
	}
	if c0["health"] != "healthy" {
		t.Errorf("container[0] health = %v, want healthy", c0["health"])
	}
	if ec, ok := c0["exit_code"]; !ok || ec != nil {
		t.Errorf("container[0] exit_code = %v, want null (running)", c0["exit_code"])
	}

	c1 := containers[1].(map[string]interface{})
	if h, ok := c1["health"]; !ok || h != nil {
		t.Errorf("container[1] health = %v, want null", c1["health"])
	}
	if ec, ok := c1["exit_code"]; !ok || ec != float64(1) {
		t.Errorf("container[1] exit_code = %v, want 1", c1["exit_code"])
	}

	platform, _ := m["platform"].(map[string]interface{})
	for _, k := range []string{"caddy_running", "caddy_routes_count", "last_backup_at",
		"last_backup_status", "agent_version", "agent_uptime_seconds"} {
		if _, ok := platform[k]; !ok {
			t.Errorf("platform missing field %q", k)
		}
	}
	if platform["agent_version"] != "1.0.0" {
		t.Errorf("platform agent_version = %v, want 1.0.0", platform["agent_version"])
	}
}
