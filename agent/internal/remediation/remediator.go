// Package remediation implements Layer 4C Step 7 — auto-remediation.
//
// For a handful of well-understood problems the agent does not just alert —
// it also takes a safe corrective action and reports what it did:
//
//	Case 1 (disk > 75%):      docker image prune -f + docker container prune -f
//	Case 2 (container crash): verify Docker's restart succeeded → "recovered"
//	Case 3 (Caddy down):      restart Caddy and restore its routes
//	Case 4 (agent memory):    flush log buffers + explicit GC
//
// Philosophy (from the spec): alert AND try to fix; never take destructive
// action automatically; always tell the user what was done. The actual Docker
// work runs in background goroutines so remediation never blocks the metrics
// collection loop; every outcome is sent as a remediation_report that the
// control plane persists as an "auto_remediation" server event.
package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/yourname/yourplatform/agent/internal/docker"
	"github.com/yourname/yourplatform/agent/internal/metrics"
)

// Cooldowns and thresholds (Step 7).
const (
	// diskRemediatePct is the trigger for automatic cleanup (matches the
	// Step 4A disk warning threshold); diskResolvePct re-arms it.
	diskRemediatePct = 75.0
	diskResolvePct   = 70.0

	// agentMemWarnMB matches the anomaly detector's agent_memory threshold.
	agentMemWarnMB = 200

	// Cooldowns prevent repeated disruptive actions.
	diskPruneCooldown = time.Hour
	caddyCooldown     = 5 * time.Minute
	memFlushCooldown  = 10 * time.Minute
)

// ReportSender sends JSON messages over the agent's WebSocket (ws.Client).
type ReportSender interface {
	SendJSON(v interface{}) error
}

// RemediationReport is the wire message telling the control plane what the
// agent did automatically (persisted as an auto_remediation server event).
type RemediationReport struct {
	Type    string             `json:"type"` // "remediation_report"
	Payload RemediationPayload `json:"payload"`
}

// RemediationPayload carries one automatic action's outcome.
type RemediationPayload struct {
	ServerID          string  `json:"server_id"`
	Action            string  `json:"action"` // docker_prune | caddy_restart | crash_recovery | memory_flush
	Success           bool    `json:"success"`
	Message           string  `json:"message"`
	Project           string  `json:"project,omitempty"`
	Container         string  `json:"container,omitempty"`
	FreedBytes        uint64  `json:"freed_bytes,omitempty"`
	DiskPercentBefore float64 `json:"disk_percent_before,omitempty"`
	DiskPercentAfter  float64 `json:"disk_percent_after,omitempty"`
	At                string  `json:"at"`
}

// Remediator evaluates each HealthReport and triggers safe corrective actions.
// It is driven from the single metrics Manager goroutine (Evaluate), but the
// actual Docker/GC work is dispatched to background goroutines so nothing here
// blocks collection. State is guarded by a mutex because of those goroutines.
type Remediator struct {
	serverID string
	sender   ReportSender
	now      func() time.Time // test seam

	// Injected actions (defaults wired in main.go; overridden in tests).
	prune          func(ctx context.Context) (*docker.PruneReport, error)
	restartCaddy   func(ctx context.Context) error
	flushLogBuffer func() int
	memStats       func() *runtime.MemStats

	mu              sync.Mutex
	diskArmed       bool
	lastDiskPrune   time.Time
	lastCaddyAction time.Time
	lastMemFlush    time.Time
	prevRestarts    map[string]int
	// recoveryPending remembers containers that crashed but were not yet
	// observed running, so the "automatically recovered" report fires on a
	// later cycle once they come back (Step 7 Case 2).
	recoveryPending map[string]bool
}

// New creates a Remediator that reports via sender.
func New(sender ReportSender, serverID string) *Remediator {
	return &Remediator{
		serverID:        serverID,
		sender:          sender,
		now:             time.Now,
		prevRestarts:    make(map[string]int),
		recoveryPending: make(map[string]bool),
	}
}

// SetPruner injects the Docker cleanup action (docker.PruneUnusedResources).
func (r *Remediator) SetPruner(fn func(ctx context.Context) (*docker.PruneReport, error)) {
	r.prune = fn
}

// SetCaddyRestarter injects the Caddy restart action. The injected function is
// responsible for restoring routes after restart (Step 7 Case 3).
func (r *Remediator) SetCaddyRestarter(fn func(ctx context.Context) error) { r.restartCaddy = fn }

// SetLogFlusher injects the log-stream buffer flush (Case 4).
func (r *Remediator) SetLogFlusher(fn func() int) { r.flushLogBuffer = fn }

// SetMemStats overrides the memory reader (tests).
func (r *Remediator) SetMemStats(fn func() *runtime.MemStats) { r.memStats = fn }

// Evaluate runs one auto-remediation pass over a freshly collected report.
func (r *Remediator) Evaluate(report metrics.HealthReport) {
	r.evalDisk(report.Server)
	r.evalCaddy(report.Platform)
	r.evalCrashRecovery(report.Containers)
	r.evalAgentMemory()
}

// --- Case 1: disk space cleanup -------------------------------------------

func (r *Remediator) evalDisk(s metrics.ServerMetrics) {
	now := r.now()
	r.mu.Lock()
	if s.DiskPercent >= diskRemediatePct && !r.diskArmed && now.Sub(r.lastDiskPrune) >= diskPruneCooldown {
		r.diskArmed = true
		r.lastDiskPrune = now
		r.mu.Unlock()
		slog.Info("disk above 75%, starting automatic cleanup", "disk_percent", s.DiskPercent)
		go r.runDiskPrune(s)
		return
	}
	// Re-arm once the disk has dropped back under the resolve threshold.
	if s.DiskPercent < diskResolvePct {
		r.diskArmed = false
	}
	r.mu.Unlock()
}

func (r *Remediator) runDiskPrune(before metrics.ServerMetrics) {
	report := RemediationReport{
		Type: "remediation_report",
		Payload: RemediationPayload{
			ServerID:          r.serverID,
			Action:            "docker_prune",
			At:                r.now().UTC().Format(time.RFC3339),
			DiskPercentBefore: before.DiskPercent,
		},
	}

	if r.prune == nil {
		report.Payload.Message = "Disk is above 75% but automatic cleanup is not configured."
		r.send(report)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res, err := r.prune(ctx)
	if err != nil {
		report.Payload.Message = "We tried to free disk space automatically but the cleanup failed."
		slog.Warn("automatic docker prune failed", "error", err)
		r.send(report)
		return
	}

	report.Payload.Success = true
	report.Payload.FreedBytes = res.SpaceReclaimedBytes
	// Estimate the new fill level from the freed bytes.
	if before.DiskTotalGB > 0 {
		after := before.DiskPercent - float64(res.SpaceReclaimedBytes)/float64(1<<30)/before.DiskTotalGB*100
		if after < 0 {
			after = 0
		}
		report.Payload.DiskPercentAfter = after
	}
	if res.SpaceReclaimedBytes == 0 {
		report.Payload.Message = "We checked for unused Docker images and containers, but there was nothing to clean up."
	} else {
		report.Payload.Message = humanBytes(res.SpaceReclaimedBytes) + " of disk space was freed automatically by removing unused Docker images and stopped containers."
	}
	slog.Info("automatic disk cleanup complete", "freed_bytes", res.SpaceReclaimedBytes)
	r.send(report)
}

// --- Case 3: Caddy down ---------------------------------------------------

func (r *Remediator) evalCaddy(p metrics.PlatformMetrics) {
	if p.CaddyRunning {
		return
	}
	r.mu.Lock()
	withinCooldown := r.now().Sub(r.lastCaddyAction) < caddyCooldown
	if withinCooldown {
		r.mu.Unlock()
		return
	}
	r.lastCaddyAction = r.now()
	r.mu.Unlock()

	slog.Warn("caddy is not running, attempting automatic restart")
	go r.runCaddyRestart()
}

func (r *Remediator) runCaddyRestart() {
	report := RemediationReport{
		Type: "remediation_report",
		Payload: RemediationPayload{
			ServerID: r.serverID,
			Action:   "caddy_restart",
			At:       r.now().UTC().Format(time.RFC3339),
		},
	}
	if r.restartCaddy == nil {
		report.Payload.Message = "The web traffic component stopped, but automatic restart is not configured."
		r.send(report)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := r.restartCaddy(ctx); err != nil {
		report.Payload.Message = "We tried to restart the web traffic component automatically, but it failed to start."
		slog.Error("automatic caddy restart failed", "error", err)
		r.send(report)
		return
	}
	report.Payload.Success = true
	report.Payload.Message = "The web traffic component stopped and was automatically restarted. Your apps should be reachable again."
	slog.Info("caddy automatically restarted")
	r.send(report)
}

// --- Case 2: container crash + restart ------------------------------------

// evalCrashRecovery verifies Docker's restart policy succeeded (Step 7 Case 2).
// When a container's restart count increases it is remembered as pending
// recovery; the "automatically recovered" report fires when the container is
// later observed running — either in the same sample or a subsequent one.
func (r *Remediator) evalCrashRecovery(containers []metrics.ContainerMetrics) {
	for _, c := range containers {
		if c.Project == "" {
			continue
		}
		key := c.Project + ":" + c.Role
		r.mu.Lock()
		prev := r.prevRestarts[key]
		justCrashed := c.RestartCount > prev
		if justCrashed {
			r.prevRestarts[key] = c.RestartCount
			// A new crash supersedes any earlier pending recovery.
			r.recoveryPending[key] = false
		}
		pending := r.recoveryPending[key]
		r.mu.Unlock()

		switch {
		case justCrashed && c.Status == "running":
			// Crashed and already back up in this sample → recovered.
			r.reportCrashRecovery(c)
		case justCrashed:
			// Crashed but not back yet → check again on a later cycle.
			r.mu.Lock()
			r.recoveryPending[key] = true
			r.mu.Unlock()
		case pending && c.Status == "running":
			// Crashed earlier, now running → recovered.
			r.reportCrashRecovery(c)
			r.mu.Lock()
			r.recoveryPending[key] = false
			r.mu.Unlock()
		case pending && (c.Status == "exited" || c.Status == "dead"):
			// Stayed down — the crash-loop alert covers it; forget the pending
			// recovery so it doesn't fire later.
			r.mu.Lock()
			r.recoveryPending[key] = false
			r.mu.Unlock()
		}
	}
}

func (r *Remediator) reportCrashRecovery(c metrics.ContainerMetrics) {
	r.send(RemediationReport{
		Type: "remediation_report",
		Payload: RemediationPayload{
			ServerID:  r.serverID,
			Action:    "crash_recovery",
			Success:   true,
			Project:   c.Project,
			Container: c.Role,
			Message:   c.Project + " crashed and was automatically restarted. It is running again.",
			At:        r.now().UTC().Format(time.RFC3339),
		},
	})
}

// --- Case 4: agent memory --------------------------------------------------

func (r *Remediator) evalAgentMemory() {
	ms := r.readMemStats()
	allocMB := ms.Alloc / (1 << 20)
	if allocMB <= agentMemWarnMB {
		return
	}

	r.mu.Lock()
	withinCooldown := r.now().Sub(r.lastMemFlush) < memFlushCooldown
	if withinCooldown {
		r.mu.Unlock()
		return
	}
	r.lastMemFlush = r.now()
	r.mu.Unlock()

	// Flush log stream buffers (the largest soft memory consumers), then run
	// an explicit GC cycle. Do NOT restart the agent — far too disruptive.
	flushed := 0
	if r.flushLogBuffer != nil {
		flushed = r.flushLogBuffer()
	}
	runtime.GC()

	after := r.readMemStats()
	afterMB := after.Alloc / (1 << 20)
	if afterMB > agentMemWarnMB {
		slog.Warn("agent memory still above threshold after flush", "mb", afterMB)
	}

	report := RemediationReport{
		Type: "remediation_report",
		Payload: RemediationPayload{
			ServerID: r.serverID,
			Action:   "memory_flush",
			Success:  true,
			Message:  "Agent memory usage is high. Log buffers were flushed and memory was cleaned up automatically.",
			At:       r.now().UTC().Format(time.RFC3339),
		},
	}
	if afterMB > agentMemWarnMB {
		report.Payload.Message = "Agent memory usage is high. Log buffers were flushed, but memory is still above 200MB — monitoring stability."
		report.Payload.Success = false
	}
	slog.Info("agent memory remediation", "flushed_buffers", flushed, "alloc_mb_before", allocMB, "alloc_mb_after", afterMB)
	r.send(report)
}

func (r *Remediator) readMemStats() *runtime.MemStats {
	if r.memStats != nil {
		return r.memStats()
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return &ms
}

func (r *Remediator) send(report RemediationReport) {
	if r.sender == nil {
		return
	}
	if err := r.sender.SendJSON(report); err != nil {
		slog.Warn("failed to send remediation report", "action", report.Payload.Action, "error", err)
	}
}

// humanBytes renders a byte count in a user-friendly form.
func humanBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
