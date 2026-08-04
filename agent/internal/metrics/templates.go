package metrics

import (
	"strings"
	"time"
)

// Step 5B — Plain-English alert templates.
//
// Every alert type has a template filled with actual values so the result is
// readable by someone who does not know what Docker is. Placeholders use
// {braces} and are filled from alertSpec.params.

// alertTemplate holds the four human-readable parts of an alert.
type alertTemplate struct {
	Title   string
	Message string
	Detail  string
	Action  string
}

// resolvedTemplate is used for every resolved alert: the subject placeholder
// names what returned to normal.
var resolvedTemplate = alertTemplate{
	Title:   "{subject} is back to normal",
	Message: "Good news — {subject} is back to normal. No action needed.",
}

// alertTemplates maps alert type → severity → template.
var alertTemplates = map[string]map[string]alertTemplate{
	"container_stopped": {
		"warning": {
			Title:   "{project} has stopped unexpectedly",
			Message: "Your app {project} stopped running and could not be automatically restarted. Your site is currently unreachable.",
			Detail:  "The app exited with error code {exit_code}.",
			Action:  "Check the logs for {project} to see what caused the crash. Common causes: missing environment variable, cannot connect to database, or a bug in your application code.",
		},
	},
	"container_oom": {
		"critical": {
			Title:   "{project} ran out of memory and was restarted",
			Message: "Your app {project} used more memory than its {limit}MB limit. It was automatically stopped and restarted. Your site should be accessible again.",
			Detail:  "Memory usage: {used}MB of {limit}MB limit.",
			Action:  "If this happens often, increase the memory limit in your app settings. You can also check if your app has a memory leak.",
		},
	},
	"container_crash": {
		"warning": {
			Title:   "{project} crashed and was restarted",
			Message: "Your app {project} crashed and was automatically restarted. Your site should be accessible again.",
			Detail:  "Restart count: {count}.",
			Action:  "Check the logs for {project} to see what caused the crash. Common causes: missing environment variable, cannot connect to database, or a bug in your application code.",
		},
		"critical": {
			Title:   "{project} keeps crashing",
			Message: "Your app {project} has crashed {count} times in the last 5 minutes. This usually means there is a problem that prevents it from starting correctly.",
			Detail:  "Restart count: {count}. Last exit code: {exit_code}.",
			Action:  "Check the logs for {project} to see the startup error. Common causes: wrong environment variable, database not available, or a bug introduced in the latest deployment. Consider rolling back to the previous version.",
		},
	},
	"disk": {
		"warning": {
			Title:   "Your server disk is getting full",
			Message: "Your server's disk is {percent}% full ({used}GB of {total}GB used). At the current rate, it will be full in approximately {days} days.",
			Detail:  "Available: {available}GB remaining.",
			Action:  "You can free up space by: removing unused Docker images (we can do this automatically), deleting old deployment logs, or upgrading your server's disk size with your hosting provider.",
		},
		"critical": {
			Title:   "Your server disk is almost full — action required",
			Message: "Your server's disk is {percent}% full. Apps may stop working soon if this is not resolved.",
			Detail:  "Only {available}GB remaining of {total}GB total.",
			Action:  "Immediate action needed. We have automatically cleaned up unused Docker images. If the disk is still full, please upgrade your server's disk or contact support.",
		},
	},
	"ram": {
		"warning": {
			Title:   "Your server is running low on memory",
			Message: "Your server's memory is {percent}% used ({used}MB of {total}MB). Apps may slow down.",
			Detail:  "Memory used: {used}MB of {total}MB.",
			Action:  "Monitor your server's memory. If it keeps growing, consider stopping unused apps or upgrading your server.",
		},
		"critical": {
			Title:   "Your server is almost out of memory",
			Message: "Your server's memory is {percent}% used. Apps may stop working soon if this is not resolved.",
			Detail:  "Only {available}MB remaining of {total}MB total.",
			Action:  "Immediate action needed. Stop unused apps or upgrade your server's memory.",
		},
	},
	"cpu": {
		"warning": {
			Title:   "Your server CPU is under heavy load",
			Message: "Your server's CPU has been above 80% for 5 minutes (currently {percent}%). Apps may respond slowly.",
			Detail:  "CPU usage: {percent}%.",
			Action:  "Check which apps are using the most CPU in the Logs tab, or consider upgrading your server.",
		},
		"critical": {
			Title:   "Your server CPU is almost maxed out",
			Message: "Your server's CPU has been above 95% for 2 minutes (currently {percent}%). Apps may be unresponsive.",
			Detail:  "CPU usage: {percent}%.",
			Action:  "Immediate action needed. Investigate the app consuming the CPU or upgrade your server.",
		},
	},
	"load": {
		"warning": {
			Title:   "Your server is under heavy load",
			Message: "Your server's load is {value} per core (warning threshold 0.8).",
			Detail:  "Load per core: {value}.",
			Action:  "Check for runaway processes or high traffic in the Logs tab.",
		},
		"critical": {
			Title:   "Your server is overloaded",
			Message: "Your server's load is {value} per core (critical threshold 1.5). Apps may be unresponsive.",
			Detail:  "Load per core: {value}.",
			Action:  "Immediate action needed. Consider stopping unused apps or upgrading your server.",
		},
	},
	"container_ram": {
		"warning": {
			Title:   "{project} is using a lot of memory",
			Message: "Your app {project} is using {used}MB of its {limit}MB memory limit ({percent}% used).",
			Detail:  "If usage reaches 100%, the app will be automatically restarted.",
			Action:  "Monitor if this continues to increase. If it does, consider increasing the memory limit or investigating memory usage.",
		},
		"critical": {
			Title:   "{project} is about to run out of memory",
			Message: "Your app {project} is using {used}MB of its {limit}MB memory limit ({percent}% used).",
			Detail:  "If usage reaches 100%, the app will be automatically restarted.",
			Action:  "Increase the memory limit for {project} in your deployment settings, or investigate memory usage in your app.",
		},
	},
	"container_unhealthy": {
		"warning": {
			Title:   "{project} health check is failing",
			Message: "Your app {project} is running but its health check is failing. It may not be serving requests correctly.",
			Detail:  "Health check status: unhealthy.",
			Action:  "Check the logs for {project} to see what is wrong, then redeploy if needed.",
		},
	},
	"backup_overdue": {
		"warning": {
			Title:   "Backup is overdue",
			Message: "Your last successful backup was {hours} hours ago. Backups normally run every 24 hours.",
			Detail:  "Last backup: {last_backup_at}. Status: {status}.",
			Action:  "You can run a backup manually from the Backups tab. If backups keep failing, check that your backup storage is accessible and has enough space.",
		},
		"critical": {
			Title:   "Backup has not run in over 2 days",
			Message: "Your last successful backup was {hours} hours ago. Your data is at risk.",
			Detail:  "Last backup: {last_backup_at}. Status: {status}.",
			Action:  "Run a backup manually from the Backups tab immediately, or check that your backup storage is accessible.",
		},
	},
	"caddy_down": {
		"critical": {
			Title:   "All your apps are unreachable",
			Message: "The component that handles web traffic (the reverse proxy) has stopped. None of your apps are accessible from the internet.",
			Detail:  "Attempting to restart automatically.",
			Action:  "This is being handled automatically. If your apps do not come back online within 2 minutes, please contact support.",
		},
	},
	"agent_memory": {
		"warning": {
			Title:   "YourPlatform agent is using a lot of memory",
			Message: "The monitoring agent on your server is using {mb}MB of memory. It is monitoring stability and has flushed log buffers to free memory.",
			Detail:  "Agent memory: {mb}MB.",
			Action:  "This is being handled automatically. If memory stays high, consider reducing the number of active log streams.",
		},
	},
}

// renderAlert turns an alertSpec into a fully-populated Alert using the
// templates. target is the state-machine target that triggered the alert.
// reuseID is the id of the machine's previously-active alert: escalations and
// resolutions reuse it (Step 5C rules 4-5) so the control plane updates the
// same row instead of leaving stale active rows. prevSev is the severity the
// machine was in before this transition, so a resolved alert reports the
// severity it is resolving.
func (d *AnomalyDetector) renderAlert(spec alertSpec, target alertSeverity, reuseID string, prevSev alertSeverity) Alert {
	now := d.now().UTC()
	status := "active"
	var resolvedAt *string
	level := levelFromTarget(target)
	severity := severityFromTarget(target)
	if isResolvedTarget(target) {
		status = "resolved"
		rs := now.Format(time.RFC3339)
		resolvedAt = &rs
		// A resolved alert reports the severity it is clearing.
		severity = severityFromTarget(prevSev)
	}
	id := newAlertID()
	if reuseID != "" {
		id = reuseID
	}

	// Pick the template: resolved alerts use the generic resolved template.
	var tmpl alertTemplate
	if isResolvedTarget(target) {
		tmpl = alertTemplate{
			Title:   resolvedTemplate.Title,
			Message: resolvedTemplate.Message,
		}
	} else {
		sevTemplates, ok := alertTemplates[spec.typ]
		if !ok {
			sevTemplates = map[string]alertTemplate{
				"warning":  {Title: spec.subject, Message: "There is a problem with {subject}."},
				"critical": {Title: spec.subject, Message: "There is a critical problem with {subject}."},
			}
		}
		sev := sevNameWarning
		if target == sevCritical {
			sev = sevNameCritical
		}
		tmpl = sevTemplates[sev]
	}

	params := spec.params
	if params == nil {
		params = map[string]string{}
	}
	if _, ok := params["subject"]; !ok {
		params["subject"] = spec.subject
	}

	return Alert{
		ID:         id,
		ServerID:   d.serverID,
		Project:    spec.project,
		Container:  spec.container,
		Level:      level,
		Severity:   severity,
		Type:       spec.typ,
		Status:     status,
		Title:      fillTemplate(tmpl.Title, params),
		Message:    fillTemplate(tmpl.Message, params),
		Detail:     fillTemplate(tmpl.Detail, params),
		Action:     fillTemplate(tmpl.Action, params),
		FiredAt:    now.Format(time.RFC3339),
		ResolvedAt: resolvedAt,
		Metrics:    spec.metrics,
	}
}

// fillTemplate substitutes {placeholders} with values from params.
func fillTemplate(s string, params map[string]string) string {
	if !strings.Contains(s, "{") {
		return s
	}
	for k, v := range params {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}
