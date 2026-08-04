package metrics

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Layer 4C Step 4 — Anomaly Detection.
//
// After every metrics collection run, the AnomalyDetector compares the current
// report against thresholds (Step 4A), drives one state machine per monitored
// metric (Step 4B), and emits an alert only on a state transition — so a
// persistent problem alerts once, alerts again on escalation or resolution,
// and never spams duplicates.

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
	cpuWarnPct      = 80.0
	cpuCritPct      = 95.0
	cpuResolvePct   = 70.0
	cpuWarnSamples  = 10
	cpuCritSamples  = 4

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
)

// AnomalyAlert is the payload sent to the control plane as an
// {"type":"anomaly_alert","payload":{...}} message.
type AnomalyAlert struct {
	Level     string `json:"level"`               // "warning" | "critical" | "resolved"
	Type      string `json:"type"`                // e.g. "cpu", "ram", "container_oom", "caddy_down"
	Project   string `json:"project,omitempty"`   // container alerts only
	Container string `json:"container,omitempty"` // container role (app, postgres, ...)
	Message   string `json:"message"`             // plain-English explanation
}

type alertSeverity int

const (
	sevNormal alertSeverity = iota
	sevWarning
	sevCritical
)

// metricState is one alert state machine (Step 4B). It also carries the CPU
// sustained-duration sample counters (Step 4C).
type metricState struct {
	sev alertSeverity

	// CPU sustained-duration counters (consecutive 30s samples).
	warnSamples int
	critSamples int
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
	machines map[string]*metricState
	crashes  map[string]*crashTracker
	now      func() time.Time // test seam
}

// NewAnomalyDetector creates a detector that sends alerts via sender.
func NewAnomalyDetector(sender WSSender) *AnomalyDetector {
	return &AnomalyDetector{
		sender:   sender,
		machines: make(map[string]*metricState),
		crashes:  make(map[string]*crashTracker),
		now:      time.Now,
	}
}

// Evaluate runs one detection pass over a freshly collected report.
func (d *AnomalyDetector) Evaluate(r HealthReport) {
	d.evalServer(r.Server)
	d.evalContainers(r.Containers)
	d.evalPlatform(r.Platform)
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
// De-escalation (CRITICAL→WARNING) is silent. build produces the alert message
// for a given target level (sevNormal = resolved message).
func (d *AnomalyDetector) transition(key string, st *metricState, target alertSeverity, build func(alertSeverity) AnomalyAlert) {
	if target == st.sev {
		return // no transition → no duplicate alert
	}

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
	d.send(build(target))
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
	d.transition(key, st, target, func(lvl alertSeverity) AnomalyAlert {
		switch lvl {
		case sevWarning:
			return AnomalyAlert{Level: sevNameWarning, Type: "cpu",
				Message: fmt.Sprintf("CPU usage has been above %.0f%% for 5 minutes (currently %.1f%%)", cpuWarnPct, v)}
		case sevCritical:
			return AnomalyAlert{Level: sevNameCritical, Type: "cpu",
				Message: fmt.Sprintf("CPU usage has been above %.0f%% for 2 minutes (currently %.1f%%)", cpuCritPct, v)}
		default:
			return AnomalyAlert{Level: sevNameResolved, Type: "cpu",
				Message: fmt.Sprintf("CPU usage is back to normal (currently %.1f%%)", v)}
		}
	})

	// --- RAM, disk, load: immediate thresholds with hysteresis ---
	d.evalThreshold("ram", "ram", s.RAMPercent, ramWarnPct, ramCritPct, ramResolvePct, "RAM usage", "%")
	d.evalThreshold("disk", "disk", s.DiskPercent, diskWarnPct, diskCritPct, diskResolvePct, "Disk usage", "%")
	d.evalThreshold("load", "load", s.LoadPerCore, loadWarn, loadCrit, loadResolve, "System load per core", "")
}

// evalThreshold is the generic immediate-threshold machine. `unit` is "%" for
// percentages or "" for ratios such as load.
func (d *AnomalyDetector) evalThreshold(key, typ string, value, warn, crit, resolve float64, name, unit string) {
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

	d.transition(key, st, target, func(lvl alertSeverity) AnomalyAlert {
		switch lvl {
		case sevWarning:
			return AnomalyAlert{Level: sevNameWarning, Type: typ,
				Message: fmt.Sprintf("%s is at %.1f%s (warning threshold: %.1f%s)", name, value, unit, warn, unit)}
		case sevCritical:
			return AnomalyAlert{Level: sevNameCritical, Type: typ,
				Message: fmt.Sprintf("%s is at %.1f%s (critical threshold: %.1f%s)", name, value, unit, crit, unit)}
		default:
			return AnomalyAlert{Level: sevNameResolved, Type: typ,
				Message: fmt.Sprintf("%s is back to normal (currently %.1f%s)", name, value, unit)}
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
		d.transition(oomKey, d.machine(oomKey), oomTarget, func(lvl alertSeverity) AnomalyAlert {
			switch lvl {
			case sevCritical:
				msg := fmt.Sprintf("%s (%s) ran out of memory and was killed (exit code %d).", c.Project, c.Role, oomExitCode)
				if c.RAMLimitMB > 0 {
					msg += fmt.Sprintf(" Memory limit: %dMB, current usage: %dMB.", c.RAMLimitMB, c.RAMUsedMB)
				}
				return AnomalyAlert{Level: sevNameCritical, Type: "container_oom", Project: c.Project, Container: c.Role, Message: msg}
			default:
				return AnomalyAlert{Level: sevNameResolved, Type: "container_oom", Project: c.Project, Container: c.Role,
					Message: fmt.Sprintf("%s (%s) is running normally again", c.Project, c.Role)}
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
		d.transition(crashKey, st, target, func(lvl alertSeverity) AnomalyAlert {
			switch lvl {
			case sevCritical:
				return AnomalyAlert{Level: sevNameCritical, Type: "container_crash", Project: c.Project, Container: c.Role,
					Message: fmt.Sprintf("%s (%s) is repeatedly crashing (%d restarts in the last 5 minutes). Check the logs for errors.", c.Project, c.Role, len(ct.events))}
			case sevWarning:
				return AnomalyAlert{Level: sevNameWarning, Type: "container_crash", Project: c.Project, Container: c.Role,
					Message: fmt.Sprintf("%s (%s) crashed and was automatically restarted (restart #%d).", c.Project, c.Role, c.RestartCount)}
			default:
				return AnomalyAlert{Level: sevNameResolved, Type: "container_crash", Project: c.Project, Container: c.Role,
					Message: fmt.Sprintf("%s (%s) is stable again — no crashes in the last 5 minutes", c.Project, c.Role)}
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
		d.transition(stoppedKey, d.machine(stoppedKey), stoppedTarget, func(lvl alertSeverity) AnomalyAlert {
			switch lvl {
			case sevWarning:
				return AnomalyAlert{Level: sevNameWarning, Type: "container_stopped", Project: c.Project, Container: c.Role,
					Message: fmt.Sprintf("%s (%s) has stopped (exit code %d).", c.Project, c.Role, *c.ExitCode)}
			default:
				return AnomalyAlert{Level: sevNameResolved, Type: "container_stopped", Project: c.Project, Container: c.Role,
					Message: fmt.Sprintf("%s (%s) is running again", c.Project, c.Role)}
			}
		})

		// Unhealthy health check → warning.
		unhealthyKey := key + ":unhealthy"
		unhealthy := c.Status == "running" && c.Health != nil && *c.Health == "unhealthy"
		unhealthyTarget := sevNormal
		if unhealthy {
			unhealthyTarget = sevWarning
		}
		d.transition(unhealthyKey, d.machine(unhealthyKey), unhealthyTarget, func(lvl alertSeverity) AnomalyAlert {
			switch lvl {
			case sevWarning:
				return AnomalyAlert{Level: sevNameWarning, Type: "container_unhealthy", Project: c.Project, Container: c.Role,
					Message: fmt.Sprintf("%s (%s) health check is failing.", c.Project, c.Role)}
			default:
				return AnomalyAlert{Level: sevNameResolved, Type: "container_unhealthy", Project: c.Project, Container: c.Role,
					Message: fmt.Sprintf("%s (%s) health check is passing again", c.Project, c.Role)}
			}
		})

		// Container memory pressure relative to its limit.
		if c.RAMLimitMB > 0 {
			d.evalThreshold(key+":ram", "container_ram", c.RAMPercent,
				containerRAMWarnPct, containerRAMCritPct, containerRAMResolvePct,
				fmt.Sprintf("%s (%s) memory usage", c.Project, c.Role), "%")
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
	d.transition(key, d.machine(key), target, func(lvl alertSeverity) AnomalyAlert {
		switch lvl {
		case sevCritical:
			return AnomalyAlert{Level: sevNameCritical, Type: "caddy_down",
				Message: "Caddy is not running — all apps are unreachable. Check the agent logs; it will try to restart Caddy automatically."}
		default:
			return AnomalyAlert{Level: sevNameResolved, Type: "caddy_down",
				Message: "Caddy is back online — all apps are reachable again"}
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
	d.transition(key, d.machine(key), target, func(lvl alertSeverity) AnomalyAlert {
		switch lvl {
		case sevWarning:
			return AnomalyAlert{Level: sevNameWarning, Type: "backup_overdue",
				Message: fmt.Sprintf("Backup is overdue — the last backup was %.0f hours ago.", ageH)}
		case sevCritical:
			return AnomalyAlert{Level: sevNameCritical, Type: "backup_overdue",
				Message: fmt.Sprintf("Backup has not run in %.0f hours (more than 2 days). Your data is at risk.", ageH)}
		default:
			return AnomalyAlert{Level: sevNameResolved, Type: "backup_overdue",
				Message: "Backup is up to date"}
		}
	})
}

// send emits one anomaly alert to the control plane.
func (d *AnomalyDetector) send(a AnomalyAlert) {
	if d.sender == nil {
		return
	}
	msg := map[string]interface{}{
		"type":    "anomaly_alert",
		"payload": a,
	}
	if err := d.sender.SendJSON(msg); err != nil {
		slog.Warn("failed to send anomaly alert", "type", a.Type, "level", a.Level, "error", err)
		return
	}
	// Drop the resolved noise from logs; keep warning/critical visible.
	if a.Level != sevNameResolved {
		slog.Info("anomaly alert", "level", a.Level, "type", a.Type,
			"project", strings.TrimSpace(a.Project), "container", strings.TrimSpace(a.Container))
	}
}
