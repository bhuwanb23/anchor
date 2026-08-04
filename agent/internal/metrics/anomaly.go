package metrics

import (
	"fmt"
	"runtime"
	"time"
)

// Layer 4C Step 4 — Anomaly Detection.
//
// After every metrics collection run, the AnomalyDetector compares the current
// report against thresholds (Step 4A), drives one state machine per monitored
// metric (Step 4B), and emits an alert only on a state transition — so a
// persistent problem alerts once, alerts again on escalation or resolution,
// and never spams duplicates.
//
// Layer 4C Step 5 — Alert Generation.
//
// The detector emits rich, plain-English Alert structs (Step 5A/5B), dedupes
// them via the state machines (Step 5C), and rate limits them per project /
// per server (Step 5D).

// Alert severity levels carried on the wire.
const (
	sevNameWarning  = "warning"
	sevNameCritical = "critical"
	sevNameResolved = "resolved"
)

// Server + platform thresholds (Step 4A).
const (
	// CPU: sustained-duration thresholds — 10 consecutive 30s samples above
	// 80% (5 minutes) for warning, 4 consecutive samples above 95% (2 minutes)
	// for critical. Resolution below 70%.
	cpuWarnPct     = 80.0
	cpuCritPct     = 95.0
	cpuResolvePct  = 70.0
	cpuWarnSamples = 10
	cpuCritSamples = 4

	ramWarnPct    = 80.0
	ramCritPct    = 90.0
	ramResolvePct = 75.0

	diskWarnPct    = 75.0
	diskCritPct    = 90.0
	diskResolvePct = 70.0

	loadWarn    = 0.8
	loadCrit    = 1.5
	loadResolve = 0.6

	// Container RAM relative to the container's memory limit.
	containerRAMWarnPct    = 80.0
	containerRAMCritPct    = 95.0
	containerRAMResolvePct = 70.0

	// Backup overdue thresholds (hours since last backup).
	backupOverdueWarnHours = 26
	backupOverdueCritHours = 50

	// Crash-loop detection: 3+ restarts within 5 minutes.
	crashLoopMin    = 3
	crashLoopWindow = 5 * time.Minute

	oomExitCode = 137

	// Step 7 Case 4 — the agent's own memory usage. Warn above 200MB (of its
	// ~256MB budget), resolve below 180MB.
	agentMemWarnMB    = 200
	agentMemResolveMB = 180
)

type alertSeverity int

const (
	sevNormal alertSeverity = iota
	sevWarning
	sevCritical
)

// metricState is one alert state machine (Step 4B). It also carries the CPU
// sustained-duration sample counters (Step 4C) and the id of the most recent
// active alert (Step 5C) so escalations and resolutions update the same row
// in the control plane's alerts table instead of leaking stale active rows.
type metricState struct {
	sev alertSeverity

	// CPU sustained-duration counters (consecutive 30s samples).
	warnSamples int
	critSamples int

	// alertID is the id of the currently-active alert for this machine. It is
	// reused by the next escalation or resolution event (Step 5C rules 4-5).
	alertID string
}

// crashTracker remembers restart events per container so crash-loop detection
// can count restarts inside a rolling 5-minute window (Step 4D).
type crashTracker struct {
	prevRestart int
	events      []time.Time
}

// AnomalyDetector evaluates each HealthReport and emits alerts on transitions.
// It is safe to use from a single goroutine only (the metrics Manager loop) —
// the same contract as SystemCollector — so no locking is needed.
type AnomalyDetector struct {
	sender   WSSender
	serverID string
	machines map[string]*metricState
	crashes  map[string]*crashTracker
	rates    map[string]*rateBucket
	now      func() time.Time         // test seam
	memStats func() *runtime.MemStats // test seam (default: runtime.ReadMemStats)
}

// NewAnomalyDetector creates a detector that sends alerts via sender.
func NewAnomalyDetector(sender WSSender, serverID string) *AnomalyDetector {
	return &AnomalyDetector{
		sender:   sender,
		serverID: serverID,
		machines: make(map[string]*metricState),
		crashes:  make(map[string]*crashTracker),
		rates:    make(map[string]*rateBucket),
		now:      time.Now,
	}
}

// Evaluate runs one detection pass over a freshly collected report.
func (d *AnomalyDetector) Evaluate(r HealthReport) {
	d.evalServer(r.Server)
	d.evalContainers(r.Containers)
	d.evalPlatform(r.Platform)
	d.evalAgentMemory()
}

// evalAgentMemory monitors the agent's own process memory (Step 7 Case 4).
// It is a plain threshold machine with hysteresis: warning above
// agentMemWarnMB, resolved below agentMemResolveMB. The auto-remediation
// manager performs the actual flush + GC; this machine just alerts.
func (d *AnomalyDetector) evalAgentMemory() {
	var ms *runtime.MemStats
	if d.memStats != nil {
		ms = d.memStats()
	} else {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		ms = &m
	}
	mb := float64(ms.Alloc) / (1 << 20)

	key := "agent_memory"
	st := d.machine(key)
	target := st.sev
	switch {
	case mb >= agentMemWarnMB:
		target = sevWarning
	case st.sev != sevNormal && mb < agentMemResolveMB:
		target = sevNormal
	}
	d.transition(key, st, target, func(lvl alertSeverity) alertSpec {
		return alertSpec{
			typ:     "agent_memory",
			subject: "YourPlatform agent memory usage",
			params:  map[string]string{"mb": fmt.Sprintf("%.0f", mb)},
			metrics: map[string]interface{}{"agent_memory_mb": int64(mb)},
		}
	})
}

func (d *AnomalyDetector) machine(key string) *metricState {
	st, ok := d.machines[key]
	if !ok {
		st = &metricState{}
		d.machines[key] = st
	}
	return st
}

func (d *AnomalyDetector) crash(key string) *crashTracker {
	ct, ok := d.crashes[key]
	if !ok {
		ct = &crashTracker{}
		d.crashes[key] = ct
	}
	return ct
}

// transition drives a single state machine toward target. Alerts fire only on
// transitions per Step 4B: NORMAL→WARNING, NORMAL→CRITICAL (skips warning),
// WARNING→CRITICAL (escalation), and any non-NORMAL→NORMAL (resolved).
// De-escalation (CRITICAL→WARNING) is silent. build produces the alert spec
// for a given target level (sevNormal = resolved message).
func (d *AnomalyDetector) transition(key string, st *metricState, target alertSeverity, build func(alertSeverity) alertSpec) {
	if target == st.sev {
		return // no transition → no duplicate alert
	}
	prevSev := st.sev

	switch {
	case target == sevNormal:
		// Resolved: the metric returned to normal.
	case st.sev == sevNormal || target > st.sev:
		// New alert (NORMAL→WARNING/CRITICAL) or escalation (WARNING→CRITICAL).
	default:
		// Silent de-escalation (CRITICAL→WARNING); stay alerting, no event.
		st.sev = target
		return
	}

	st.sev = target
	d.fire(st, build(target), target, prevSev)
}

// fire renders, rate limits, and emits one alert for the given target state.
// Escalations and resolutions reuse the machine's active alert id so the
// control plane updates the same row (Step 5C rules 4-5).
func (d *AnomalyDetector) fire(st *metricState, spec alertSpec, target alertSeverity, prevSev alertSeverity) {
	a := d.renderAlert(spec, target, st.alertID, prevSev)
	if !d.rateLimited(a) {
		return
	}
	d.emit(a)
	if a.Status == "active" {
		st.alertID = a.ID
	} else {
		// Resolved: the condition cleared, so the id is no longer active.
		st.alertID = ""
	}
}

// evalServer evaluates host-level metrics: CPU (sustained), RAM, disk, load.
func (d *AnomalyDetector) evalServer(s ServerMetrics) {
	// --- CPU: sustained-duration detection (Step 4C) ---
	key := "cpu"
	st := d.machine(key)
	v := s.CPUPercent
	// Sustained-duration counters (Step 4C). While the value is elevated but
	// the required number of consecutive samples hasn't accumulated yet, the
	// state is held (target stays at the current severity) rather than being
	// reset to NORMAL — that would emit a false "resolved" during escalation.
	// Resolution hysteresis: below the warn band, the abnormal state is held
	// until the value drops under cpuResolvePct (70%) so the alert doesn't
	// flap at the 80% boundary.
	target := st.sev
	switch {
	case v >= cpuCritPct:
		st.critSamples++
		st.warnSamples = 0
		if st.critSamples >= cpuCritSamples {
			target = sevCritical
		}
	case v >= cpuWarnPct:
		st.warnSamples++
		st.critSamples = 0
		if st.warnSamples >= cpuWarnSamples {
			target = sevWarning
		}
	default:
		st.warnSamples = 0
		st.critSamples = 0
		if st.sev == sevNormal || v < cpuResolvePct {
			target = sevNormal
		}
	}
	d.transition(key, st, target, func(lvl alertSeverity) alertSpec {
		return alertSpec{
			typ:     "cpu",
			subject: "your server's CPU usage",
			params:  map[string]string{"percent": fmt.Sprintf("%.1f", v)},
			metrics: map[string]interface{}{"cpu_percent": v},
		}
	})

	// --- RAM, disk, load: immediate thresholds with hysteresis ---
	ramAvail := s.RAMTotalMB - s.RAMUsedMB
	if ramAvail < 0 {
		ramAvail = 0
	}
	d.evalThreshold("ram", "ram", "your server's memory usage", s.RAMPercent, ramWarnPct, ramCritPct, ramResolvePct,
		func() map[string]string {
			return map[string]string{
				"percent":   fmt.Sprintf("%.1f", s.RAMPercent),
				"used":      fmt.Sprintf("%d", s.RAMUsedMB),
				"total":     fmt.Sprintf("%d", s.RAMTotalMB),
				"available": fmt.Sprintf("%d", ramAvail),
			}
		},
		map[string]interface{}{
			"ram_used_mb":      s.RAMUsedMB,
			"ram_total_mb":     s.RAMTotalMB,
			"ram_percent":      s.RAMPercent,
			"ram_available_mb": ramAvail,
		})

	diskAvail := s.DiskTotalGB - s.DiskUsedGB
	if diskAvail < 0 {
		diskAvail = 0
	}
	days := 0.0
	if s.DiskUsedGB > 0 && s.DiskPercent > 0 {
		// Rough projection: linear extrapolation from the current usage level.
		remainingPct := 100.0 - s.DiskPercent
		if remainingPct > 0 {
			days = remainingPct / (s.DiskPercent / 30.0) // assumes ~30d to reach current fill
		}
	}
	d.evalThreshold("disk", "disk", "your server's disk", s.DiskPercent, diskWarnPct, diskCritPct, diskResolvePct,
		func() map[string]string {
			return map[string]string{
				"percent":   fmt.Sprintf("%.1f", s.DiskPercent),
				"used":      fmt.Sprintf("%.1f", s.DiskUsedGB),
				"total":     fmt.Sprintf("%.1f", s.DiskTotalGB),
				"available": fmt.Sprintf("%.1f", diskAvail),
				"days":      fmt.Sprintf("%.0f", days),
			}
		},
		map[string]interface{}{
			"disk_used_gb":      s.DiskUsedGB,
			"disk_total_gb":     s.DiskTotalGB,
			"disk_percent":      s.DiskPercent,
			"disk_available_gb": diskAvail,
		})

	d.evalThreshold("load", "load", "your server's load", s.LoadPerCore, loadWarn, loadCrit, loadResolve,
		func() map[string]string {
			return map[string]string{"value": fmt.Sprintf("%.2f", s.LoadPerCore)}
		},
		map[string]interface{}{"load_per_core": s.LoadPerCore})
}

// evalThreshold is the generic immediate-threshold machine. buildParams
// returns the template parameters for the current value.
func (d *AnomalyDetector) evalThreshold(key, typ, subject string, value, warn, crit, resolve float64, buildParams func() map[string]string, metrics map[string]interface{}) {
	st := d.machine(key)

	target := sevNormal
	switch {
	case value >= crit:
		target = sevCritical
	case value >= warn:
		target = sevWarning
	}
	// Hysteresis: hold the current abnormal state until the value drops below
	// the resolution threshold, so alerts don't flap at the boundary.
	if st.sev != sevNormal && target == sevNormal && value >= resolve {
		target = st.sev
	}

	d.transition(key, st, target, func(lvl alertSeverity) alertSpec {
		return alertSpec{
			typ:     typ,
			subject: subject,
			params:  buildParams(),
			metrics: metrics,
		}
	})
}

// evalContainers detects per-container problems: OOM, crashes / crash loops,
// stopped containers, unhealthy health checks, and memory pressure.
func (d *AnomalyDetector) evalContainers(containers []ContainerMetrics) {
	for _, c := range containers {
		if c.Project == "" {
			continue
		}
		key := "container:" + c.Project + ":" + c.Role
		now := d.now()
		subject := fmt.Sprintf("%s (%s)", c.Project, c.Role)

		// --- Crash detection (Step 4D) ---
		ct := d.crash(key)
		delta := c.RestartCount - ct.prevRestart
		ct.prevRestart = c.RestartCount
		if delta > 0 {
			for i := 0; i < delta; i++ {
				ct.events = append(ct.events, now)
			}
		}
		// Prune restarts outside the 5-minute crash-loop window.
		cutoff := now.Add(-crashLoopWindow)
		kept := ct.events[:0]
		for _, t := range ct.events {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		ct.events = kept

		// OOM kill → critical, distinct from a generic crash.
		//
		// NOTE: OOM detection is polling-based — a container that is killed
		// and auto-restarts between two 30s samples may never be observed
		// with OOMKilled=true or exit 137. A Docker events stream would be
		// the robust alternative (future enhancement).
		oomKey := key + ":oom"
		oom := c.OOMKilled || (c.ExitCode != nil && *c.ExitCode == oomExitCode)
		oomTarget := sevNormal
		if oom {
			oomTarget = sevCritical
		}
		d.transition(oomKey, d.machine(oomKey), oomTarget, func(lvl alertSeverity) alertSpec {
			exitCode := 0
			if c.ExitCode != nil {
				exitCode = *c.ExitCode
			}
			return alertSpec{
				typ:       "container_oom",
				project:   c.Project,
				container: c.Role,
				subject:   subject,
				params: map[string]string{
					"project":   c.Project,
					"limit":     fmt.Sprintf("%d", c.RAMLimitMB),
					"used":      fmt.Sprintf("%d", c.RAMUsedMB),
					"exit_code": fmt.Sprintf("%d", exitCode),
				},
				metrics: map[string]interface{}{
					"ram_used_mb":  c.RAMUsedMB,
					"ram_limit_mb": c.RAMLimitMB,
					"exit_code":    exitCode,
				},
			}
		})

		// Single crash (restart count increased) vs crash loop (3+ in 5 min).
		// All three levels share one wire type ("container_crash") so the
		// control plane can correlate warning → critical → resolved.
		crashKey := key + ":crash"
		st := d.machine(crashKey)
		target := st.sev
		switch {
		case oom:
			// OOM has its own specific alert (container_oom); hold the crash
			// machine so one event doesn't produce two alerts.
		case len(ct.events) >= crashLoopMin:
			target = sevCritical
		case delta > 0:
			target = sevWarning
		case len(ct.events) == 0:
			target = sevNormal
		default:
			// 1–2 crashes still inside the window: hold until the window passes
			// or more crashes arrive.
		}
		d.transition(crashKey, st, target, func(lvl alertSeverity) alertSpec {
			exitCode := 0
			if c.ExitCode != nil {
				exitCode = *c.ExitCode
			}
			return alertSpec{
				typ:       "container_crash",
				project:   c.Project,
				container: c.Role,
				subject:   subject,
				params: map[string]string{
					"project":   c.Project,
					"count":     fmt.Sprintf("%d", len(ct.events)),
					"exit_code": fmt.Sprintf("%d", exitCode),
				},
				metrics: map[string]interface{}{
					"restart_count": c.RestartCount,
					"exit_code":     exitCode,
				},
			}
		})

		// Container stopped with a non-zero exit code (and not OOM, not a
		// restart being counted). Clean stops (exit 0) are not alerted.
		stoppedKey := key + ":stopped"
		// Only alert about a stopped container when no crash is being tracked
		// (an exited state is usually the first observation of a crash that
		// Docker is about to restart) and no OOM is involved.
		stopped := (c.Status == "exited" || c.Status == "stopped" || c.Status == "dead") &&
			c.ExitCode != nil && *c.ExitCode != 0 && *c.ExitCode != oomExitCode &&
			!c.OOMKilled && delta == 0 && len(ct.events) == 0 && st.sev == sevNormal
		stoppedTarget := sevNormal
		if stopped {
			stoppedTarget = sevWarning
		}
		d.transition(stoppedKey, d.machine(stoppedKey), stoppedTarget, func(lvl alertSeverity) alertSpec {
			return alertSpec{
				typ:       "container_stopped",
				project:   c.Project,
				container: c.Role,
				subject:   subject,
				params: map[string]string{
					"project":   c.Project,
					"exit_code": fmt.Sprintf("%d", *c.ExitCode),
				},
				metrics: map[string]interface{}{"exit_code": *c.ExitCode},
			}
		})

		// Unhealthy health check → warning.
		unhealthyKey := key + ":unhealthy"
		unhealthy := c.Status == "running" && c.Health != nil && *c.Health == "unhealthy"
		unhealthyTarget := sevNormal
		if unhealthy {
			unhealthyTarget = sevWarning
		}
		d.transition(unhealthyKey, d.machine(unhealthyKey), unhealthyTarget, func(lvl alertSeverity) alertSpec {
			return alertSpec{
				typ:       "container_unhealthy",
				project:   c.Project,
				container: c.Role,
				subject:   subject,
				params:    map[string]string{"project": c.Project},
			}
		})

		// Container memory pressure relative to its limit.
		if c.RAMLimitMB > 0 {
			ramSubject := fmt.Sprintf("%s (%s) memory usage", c.Project, c.Role)
			d.evalThreshold(key+":ram", "container_ram", ramSubject, c.RAMPercent,
				containerRAMWarnPct, containerRAMCritPct, containerRAMResolvePct,
				func() map[string]string {
					return map[string]string{
						"project": c.Project,
						"used":    fmt.Sprintf("%d", c.RAMUsedMB),
						"limit":   fmt.Sprintf("%d", c.RAMLimitMB),
						"percent": fmt.Sprintf("%.1f", c.RAMPercent),
					}
				},
				map[string]interface{}{
					"ram_used_mb":  c.RAMUsedMB,
					"ram_limit_mb": c.RAMLimitMB,
					"ram_percent":  c.RAMPercent,
				})
		}
	}
}

// evalPlatform detects platform-component problems: Caddy down and overdue
// backups.
func (d *AnomalyDetector) evalPlatform(p PlatformMetrics) {
	// Caddy down → critical: all apps unreachable.
	key := "caddy"
	target := sevNormal
	if !p.CaddyRunning {
		target = sevCritical
	}
	d.transition(key, d.machine(key), target, func(lvl alertSeverity) alertSpec {
		return alertSpec{
			typ:     "caddy_down",
			subject: "your web traffic (Caddy)",
		}
	})

	// Backup overdue: warning after 26h, critical after 50h.
	key = "backup"
	ageH := float64(p.LastBackupAgeSec) / 3600
	target = sevNormal
	switch {
	case p.LastBackupAgeSec > 0 && ageH >= backupOverdueCritHours:
		target = sevCritical
	case p.LastBackupAgeSec > 0 && ageH >= backupOverdueWarnHours:
		target = sevWarning
	}
	d.transition(key, d.machine(key), target, func(lvl alertSeverity) alertSpec {
		lastBackup := p.LastBackupAt
		if lastBackup == "" {
			lastBackup = "never"
		}
		status := p.LastBackupStatus
		if status == "" {
			status = "unknown"
		}
		return alertSpec{
			typ:     "backup_overdue",
			subject: "your backups",
			params: map[string]string{
				"hours":          fmt.Sprintf("%.0f", ageH),
				"last_backup_at": lastBackup,
				"status":         status,
			},
			metrics: map[string]interface{}{"last_backup_age_seconds": p.LastBackupAgeSec},
		}
	})
}
