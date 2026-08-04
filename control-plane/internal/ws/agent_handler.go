package ws

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/yourname/yourplatform/control-plane/internal/alerts"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func handlePreflightResult(db *sql.DB, serverID string, payload json.RawMessage) {
	var result struct {
		SystemInfo struct {
			OS              string  `json:"os"`
			OSVersion       string  `json:"os_version"`
			OSPretty        string  `json:"os_pretty,omitempty"`
			Arch            string  `json:"arch"`
			RAMMB           int     `json:"ram_mb"`
			RAMAvailableMB  int     `json:"ram_available_mb"`
			DiskTotalGB     int     `json:"disk_total_gb"`
			DiskAvailableGB int     `json:"disk_available_gb"`
			DiskUsedPercent float64 `json:"disk_used_percent"`
			DockerVersion   string  `json:"docker_version,omitempty"`
		} `json:"system_info"`
		Passed    bool              `json:"passed"`
		Warnings  []json.RawMessage `json:"warnings"`
		AutoFixed []struct {
			Check     string `json:"check"`
			Action    string `json:"action"`
			Timestamp string `json:"timestamp"`
		} `json:"auto_fixed"`
	}

	if err := json.Unmarshal(payload, &result); err != nil {
		slog.Warn("failed to parse preflight_result", "server_id", serverID, "error", err)
		return
	}

	si := result.SystemInfo
	_ = queries.UpdateServerSystemInfo(db, serverID,
		si.OSVersion, si.OSPretty, si.DockerVersion,
		si.RAMAvailableMB, si.DiskTotalGB, si.DiskAvailableGB,
		si.DiskUsedPercent,
	)

	for _, fix := range result.AutoFixed {
		_ = queries.InsertServerEvent(db, uuid.New().String(), serverID, "auto_fixed", fix.Check, fix.Action, fix.Timestamp)
	}

	for _, warn := range result.Warnings {
		_ = queries.InsertServerEvent(db, uuid.New().String(), serverID, "warning", "", string(warn), "")
	}

	slog.Info("preflight result processed", "server_id", serverID, "passed", result.Passed, "warnings", len(result.Warnings))
}

func handleBackupStatus(db *sql.DB, serverID string, payload json.RawMessage) {
	var result struct {
		ServerID         string `json:"server_id"`
		BackupID         string `json:"backup_id"`
		ResticSnapshotID string `json:"restic_snapshot_id"`
		Status           string `json:"status"`
		StartedAt        string `json:"started_at"`
		CompletedAt      string `json:"completed_at,omitempty"`
		DurationSeconds  int64  `json:"duration_seconds"`
		SizeNewBytes     int64  `json:"size_new_bytes"`
		SizeTotalBytes   int64  `json:"size_total_bytes"`
		Projects         json.RawMessage `json:"projects,omitempty"`
		RetentionApplied bool   `json:"retention_applied"`
		SnapshotsPruned  int    `json:"snapshots_pruned"`
		Error            string `json:"error,omitempty"`
	}

	if err := json.Unmarshal(payload, &result); err != nil {
		slog.Warn("failed to parse backup_status", "server_id", serverID, "error", err)
		return
	}

	if result.BackupID == "" || result.Status == "" {
		return
	}

	// Find or create the job record
	jobID := result.BackupID
	existingJob, _ := queries.GetBackupJobByID(db, jobID)
	if existingJob == nil {
		// Job doesn't exist yet (might be a scheduled backup without pre-created job)
		_ = queries.InsertBackupJob(db, jobID, serverID)
	}

	var errPtr, snapPtr, projResultsPtr *string
	var durPtr, sizeNewPtr, sizeTotalPtr *int64
	var retPtr *bool
	var prunedPtr *int

	if result.Error != "" {
		errPtr = &result.Error
	}
	if result.ResticSnapshotID != "" {
		snapPtr = &result.ResticSnapshotID
	}
	if len(result.Projects) > 0 {
		projStr := string(result.Projects)
		projResultsPtr = &projStr
	}
	if result.DurationSeconds > 0 {
		durPtr = &result.DurationSeconds
	}
	if result.SizeNewBytes > 0 {
		sizeNewPtr = &result.SizeNewBytes
	}
	if result.SizeTotalBytes > 0 {
		sizeTotalPtr = &result.SizeTotalBytes
	}
	retPtr = &result.RetentionApplied
	prunedPtr = &result.SnapshotsPruned

	_ = queries.UpdateBackupJobFull(db, jobID, result.Status, durPtr, sizeNewPtr, sizeTotalPtr, projResultsPtr, retPtr, prunedPtr, errPtr, snapPtr)

	// If completed with snapshot, record snapshot
	if (result.Status == "success" || result.Status == "completed" || result.Status == "partial") && result.ResticSnapshotID != "" {
		snapshotID := uuid.New().String()
		_ = queries.InsertBackupSnapshot(db, snapshotID, serverID, result.ResticSnapshotID, "/var/lib/yourplatform", result.SizeNewBytes)
	}

	// Update last backup time on success
	if result.Status == "success" || result.Status == "completed" || result.Status == "partial" {
		_ = queries.UpdateLastBackupTime(db, serverID)
	}

	// Persist repository storage usage and evaluate plan limits
	if result.SizeTotalBytes > 0 {
		_ = queries.UpdateRepositorySize(db, serverID, result.SizeTotalBytes)
		_ = queries.InsertBackupStorageHistory(db, uuid.New().String(), serverID, result.SizeTotalBytes)
		EvaluateAndAlertStorageQuota(db, serverID, result.SizeTotalBytes)
	}

	slog.Info("backup status", "server_id", serverID, "backup_id", result.BackupID, "status", result.Status)
}

// handleRestoreStatus processes restore status updates from the agent.
func handleRestoreStatus(db *sql.DB, serverID string, payload json.RawMessage) {
	var result struct {
		ServerID        string `json:"server_id"`
		RestoreID       string `json:"restore_id"`
		SnapshotID      string `json:"snapshot_id"`
		ProjectName     string `json:"project_name"`
		Status          string `json:"status"`
		StartedAt       string `json:"started_at"`
		CompletedAt     string `json:"completed_at,omitempty"`
		DurationSeconds int64  `json:"duration_seconds"`
		Error           string `json:"error,omitempty"`
	}

	if err := json.Unmarshal(payload, &result); err != nil {
		slog.Warn("failed to parse restore_status", "server_id", serverID, "error", err)
		return
	}

	if result.RestoreID == "" || result.Status == "" {
		return
	}

	var errPtr *string
	if result.Error != "" {
		errPtr = &result.Error
	}

	_ = queries.UpdateRestoreJobStatus(db, result.RestoreID, result.Status, errPtr)

	slog.Info("restore status",
		"server_id", serverID,
		"restore_id", result.RestoreID,
		"project", result.ProjectName,
		"status", result.Status)
}

// handleBackupVerification processes backup verification updates from the agent.
func handleBackupVerification(db *sql.DB, serverID string, payload json.RawMessage) {
	var result struct {
		ServerID         string `json:"server_id"`
		BackupID         string `json:"backup_id"`
		SnapshotID       string `json:"snapshot_id"`
		Status           string `json:"status"`
		Subset           string `json:"subset"`
		StartedAt        string `json:"started_at"`
		CompletedAt      string `json:"completed_at,omitempty"`
		DurationSeconds  int64  `json:"duration_seconds"`
		FilesCount       int    `json:"files_count"`
		Error            string `json:"error,omitempty"`
	}

	if err := json.Unmarshal(payload, &result); err != nil {
		slog.Warn("failed to parse backup_verification", "server_id", serverID, "error", err)
		return
	}

	if result.BackupID == "" || result.Status == "" {
		return
	}

	// Update the backup job's verification status
	_ = queries.UpdateBackupJobVerification(db, result.BackupID, result.Status, result.Error)

	slog.Info("backup verification",
		"server_id", serverID,
		"backup_id", result.BackupID,
		"snapshot", result.SnapshotID,
		"status", result.Status,
		"subset", result.Subset,
		"files", result.FilesCount)
}

// handleAnomalyAlert persists a Layer 4C Step 4/5 anomaly alert: it upserts
// the rich Step 5 alert into the alerts table (deduped by id, escalation and
// resolution reuse the id), keeps a server_events row for backward compat,
// hands it to the Step 6 delivery manager for email, and lets the caller
// forward it to browsers for live display.
func handleAnomalyAlert(db *sql.DB, serverID string, payload json.RawMessage, delivery *alerts.Delivery) {
	var a struct {
		ID         string                 `json:"id"`
		Project    string                 `json:"project,omitempty"`
		Container  string                 `json:"container,omitempty"`
		Level      string                 `json:"level"`
		Severity   string                 `json:"severity"`
		Type       string                 `json:"type"`
		Status     string                 `json:"status"`
		Title      string                 `json:"title"`
		Message    string                 `json:"message"`
		Detail     string                 `json:"detail,omitempty"`
		Action     string                 `json:"action,omitempty"`
		FiredAt    string                 `json:"fired_at"`
		ResolvedAt *string                `json:"resolved_at,omitempty"`
		Metrics    map[string]interface{} `json:"metrics,omitempty"`
	}
	if err := json.Unmarshal(payload, &a); err != nil {
		slog.Warn("failed to parse anomaly_alert", "server_id", serverID, "error", err)
		return
	}
	if a.Message == "" && a.Title == "" {
		return
	}

	// Step 5: persist the rich alert (upsert by id so escalations/resolutions
	// update the same row).
	title := a.Title
	if title == "" {
		title = a.Message
	}
	severity := a.Severity
	if severity == "" {
		severity = a.Level
	}
	status := a.Status
	if status == "" {
		if a.Level == "resolved" {
			status = "resolved"
		} else {
			status = "active"
		}
	}
	alertID := a.ID
	if alertID == "" {
		alertID = uuid.New().String()
	}
	metricsJSON := ""
	if len(a.Metrics) > 0 {
		if b, err := json.Marshal(a.Metrics); err == nil {
			metricsJSON = string(b)
		}
	}
	resolvedAt := ""
	if a.ResolvedAt != nil {
		resolvedAt = *a.ResolvedAt
	}
	record := queries.AlertRecord{
		ID:          alertID,
		ServerID:    serverID,
		Project:     a.Project,
		Container:   a.Container,
		Severity:    severity,
		Level:       a.Level,
		Type:        a.Type,
		Status:      status,
		Title:       title,
		Message:     a.Message,
		Detail:      a.Detail,
		Action:      a.Action,
		MetricsJSON: metricsJSON,
		FiredAt:     a.FiredAt,
		ResolvedAt:  resolvedAt,
	}
	_ = queries.UpsertAlert(db, record)
	if delivery != nil {
		delivery.HandleAlert(serverID, record)
	}

	// Backward-compatible server event row.
	eventType := "warning"
	switch a.Level {
	case "critical", "resolved":
		eventType = "alert"
	}
	checkName := a.Type
	if a.Project != "" {
		checkName = a.Project + "/" + a.Type
	}
	_ = queries.InsertServerEvent(db, uuid.New().String(), serverID, eventType, checkName, title, a.Container)
	slog.Info("anomaly alert", "server_id", serverID, "level", a.Level, "type", a.Type, "status", status)
}

// healthReportContainer mirrors the agent's ContainerMetrics JSON fields.
type healthReportContainer struct {
	Project      string  `json:"project"`
	Role         string  `json:"role"`
	ContainerID  string  `json:"container_id"`
	Status       string  `json:"status"`
	Health       *string `json:"health"`
	CPUPercent   float64 `json:"cpu_percent"`
	RAMUsedMB    int64   `json:"ram_used_mb"`
	RAMLimitMB   int64   `json:"ram_limit_mb"`
	RAMPercent   float64 `json:"ram_percent"`
	RestartCount int     `json:"restart_count"`
	UptimeSecs   int64   `json:"uptime_seconds"`
	ExitCode     *int    `json:"exit_code,omitempty"`
	NetRxBytes   uint64  `json:"net_rx_bytes,omitempty"`
	NetTxBytes   uint64  `json:"net_tx_bytes,omitempty"`
}

// healthReportServer mirrors the agent's ServerMetrics JSON fields.
type healthReportServer struct {
	CPUPercent    float64 `json:"cpu_percent"`
	RAMUsedMB     int64   `json:"ram_used_mb"`
	RAMTotalMB    int64   `json:"ram_total_mb"`
	RAMPercent    float64 `json:"ram_percent"`
	DiskUsedGB    float64 `json:"disk_used_gb"`
	DiskTotalGB   float64 `json:"disk_total_gb"`
	DiskPercent   float64 `json:"disk_percent"`
	Load1Min      float64 `json:"load_1min"`
	LoadPerCore   float64 `json:"load_per_core"`
}

// healthReportPlatform mirrors the agent's PlatformMetrics JSON fields.
type healthReportPlatform struct {
	CaddyRunning     bool   `json:"caddy_running"`
	CaddyRoutesCount int    `json:"caddy_routes_count"`
	LastBackupAt     string `json:"last_backup_at,omitempty"`
	LastBackupAgeSec int64  `json:"last_backup_age_seconds"`
}

// healthReportPayload mirrors the agent's HealthReport JSON shape.
type healthReportPayload struct {
	Type          string                `json:"type"`
	ServerID      string                `json:"server_id"`
	Timestamp     string                `json:"timestamp"`
	CollectedInMS int64                 `json:"collected_in_ms"`
	Server        healthReportServer    `json:"server"`
	Containers    []healthReportContainer `json:"containers"`
	Platform      healthReportPlatform  `json:"platform"`
}

// healthReportBatchPayload wraps a batch of reports sent on reconnect.
type healthReportBatchPayload struct {
	Type     string              `json:"type"`
	ServerID string              `json:"server_id"`
	Reports  []healthReportPayload `json:"reports"`
}

func handleHealthReport(db *sql.DB, serverID string, payload json.RawMessage) {
	var r healthReportPayload
	if err := json.Unmarshal(payload, &r); err != nil {
		slog.Warn("failed to parse health_report", "server_id", serverID, "error", err)
		return
	}
	if r.ServerID != "" && r.ServerID != serverID {
		slog.Warn("health_report server_id mismatch", "wanted", serverID, "got", r.ServerID)
		return
	}

	// Update server last_seen and status
	_ = queries.UpdateServerConnection(db, serverID, "connected")

	// Upsert container statuses
	for _, c := range r.Containers {
		_ = queries.UpsertContainerStatus(db, serverID,
			c.Project, c.Role, c.ContainerID, c.Status, c.Health,
			c.CPUPercent, c.RAMUsedMB, c.RAMLimitMB, c.RAMPercent,
			c.RestartCount, c.UptimeSecs, c.ExitCode,
			c.NetRxBytes, c.NetTxBytes)
	}

	// Insert metrics summary
	ts := r.Timestamp
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}
	mid := uuid.New().String()
	srv := r.Server
	plat := r.Platform
	var backupAge *int64
	if plat.LastBackupAgeSec > 0 {
		backupAge = &plat.LastBackupAgeSec
	}
	_ = queries.InsertMetric(db, mid, serverID, ts, r.CollectedInMS,
		srv.CPUPercent, srv.RAMUsedMB, srv.RAMTotalMB, srv.RAMPercent,
		srv.DiskUsedGB, srv.DiskTotalGB, srv.DiskPercent,
		srv.Load1Min, srv.LoadPerCore,
		plat.CaddyRunning, plat.CaddyRoutesCount, backupAge,
		len(r.Containers))

	slog.Debug("health_report processed", "server_id", serverID, "containers", len(r.Containers))
}

func handleHealthReportBatch(db *sql.DB, serverID string, payload json.RawMessage) {
	var batch healthReportBatchPayload
	if err := json.Unmarshal(payload, &batch); err != nil {
		slog.Warn("failed to parse health_report_batch", "server_id", serverID, "error", err)
		return
	}
	if batch.ServerID != "" && batch.ServerID != serverID {
		slog.Warn("health_report_batch server_id mismatch", "wanted", serverID, "got", batch.ServerID)
		return
	}
	for _, r := range batch.Reports {
		// Re-marshal each report as if it were a single health_report
		raw, err := json.Marshal(r)
		if err != nil {
			continue
		}
		handleHealthReport(db, serverID, raw)
	}
	slog.Info("health_report_batch processed", "server_id", serverID, "count", len(batch.Reports))
}

func HandleAgentWS(hub *Hub, db *sql.DB, baseDomain string, delivery *alerts.Delivery) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Basic ") {
			http.Error(w, "missing basic auth", http.StatusUnauthorized)
			return
		}

		encoded := strings.TrimPrefix(authHeader, "Basic ")
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			http.Error(w, "invalid base64 auth", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) != 2 {
			http.Error(w, "invalid auth format (expected agent_id:agent_secret)", http.StatusUnauthorized)
			return
		}
		agentID, agentSecret := parts[0], parts[1]

		serverID, _, _, secretHash, status, err := queries.GetServerByAgentID(db, agentID)
		if err != nil {
			slog.Warn("agent lookup failed", "agent_id", agentID, "error", err)
			http.Error(w, "agent not found", http.StatusUnauthorized)
			return
		}

		providedHash := sha256.Sum256([]byte(agentSecret))
		providedHex := hex.EncodeToString(providedHash[:])
		if providedHex != secretHash {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("websocket upgrade", "agent_id", agentID, "error", err)
			return
		}

		hub.RegisterAgent(serverID, agentID, conn)
		slog.Info("agent connected", "agent_id", agentID, "server_id", serverID, "status", status)

		ack := map[string]string{
			"type":        "register_ack",
			"server_id":   serverID,
			"base_domain": baseDomain,
		}
		_ = conn.WriteJSON(ack)

		// Deliver offline queue
		sendHelloAck(conn, db, serverID)

		// Re-establish any live log streams dashboards were watching before this
		// agent (re)connected (Layer 4C 3B).
		hub.ReplayStreamCommands(serverID)

		if status != "connected" {
			_ = queries.UpdateServerConnection(db, serverID, "connected")
		}

		go func() {
			defer func() {
				hub.UnregisterAgent(serverID)
				_ = queries.UpdateServerConnection(db, serverID, "disconnected")
				slog.Info("agent disconnected", "agent_id", agentID, "server_id", serverID)
			}()

			conn.SetReadDeadline(time.Time{})
			for {
				_, data, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						slog.Info("agent ws closed normally", "server_id", serverID)
					} else {
						slog.Warn("agent ws read error", "server_id", serverID, "error", err)
					}
					return
				}

				var msg Message
				if err := json.Unmarshal(data, &msg); err != nil {
					slog.Warn("bad message from agent", "server_id", serverID, "error", err)
					continue
				}

			switch msg.Type {
			case "hello":
				sendHelloAck(conn, db, serverID)
			case "result":
				slog.Info("command result", "server_id", serverID, "payload", string(msg.Payload))
				hub.ForwardToBrowsers(serverID, data)
			case "command_ack":
				hub.ForwardToBrowsers(serverID, data)
				slog.Debug("command_ack", "server_id", serverID, "payload", string(msg.Payload))
			case "command_progress":
				hub.ForwardToBrowsers(serverID, data)
			case "preflight_result":
				handlePreflightResult(db, serverID, msg.Payload)
			case "certificate_alert":
				hub.ForwardToBrowsers(serverID, data)
				slog.Warn("certificate alert", "server_id", serverID, "payload", string(msg.Payload))
		case "error_alert":
			hub.ForwardToBrowsers(serverID, data)
			slog.Warn("error alert", "server_id", serverID, "payload", string(msg.Payload))
		case "anomaly_alert":
			handleAnomalyAlert(db, serverID, msg.Payload, delivery)
			hub.ForwardToBrowsers(serverID, data)
			case "server_event":
				hub.ForwardToBrowsers(serverID, data)
				slog.Info("server event", "server_id", serverID, "payload", string(msg.Payload))
			case "backup_status":
				handleBackupStatus(db, serverID, msg.Payload)
				hub.ForwardToBrowsers(serverID, data)
		case "restore_status":
			handleRestoreStatus(db, serverID, msg.Payload)
			hub.ForwardToBrowsers(serverID, data)
		case "backup_verification":
			handleBackupVerification(db, serverID, msg.Payload)
			hub.ForwardToBrowsers(serverID, data)
		case "log_line", "log_lines", "log_history", "stream_ended", "pull_progress", "docker_status", "reconciliation_result":
				hub.ForwardToBrowsers(serverID, data)
			case "health_report":
				handleHealthReport(db, serverID, msg.Payload)
				hub.ForwardToBrowsers(serverID, data)
			case "health_report_batch":
				handleHealthReportBatch(db, serverID, msg.Payload)
				hub.ForwardToBrowsers(serverID, data)
			default:
				slog.Debug("agent message", "type", msg.Type, "server_id", serverID)
			}
			}
		}()

		go func() {
			sendCh := hub.GetAgentSend(serverID)
			if sendCh == nil {
				return
			}
			for msg := range sendCh {
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					slog.Warn("ws write to agent", "server_id", serverID, "error", err)
					return
				}
			}
		}()
	}
}

func sendHelloAck(conn *websocket.Conn, db *sql.DB, serverID string) {
	pending, err := queries.PendingCommandsAsJSON(db, serverID)
	if err != nil {
		slog.Warn("list pending commands", "server_id", serverID, "error", err)
		pending = nil
	}
	if pending == nil {
		pending = []json.RawMessage{}
	}
	ack := map[string]interface{}{
		"type": "hello_ack",
		"payload": map[string]interface{}{
			"server_id":         serverID,
			"pending_commands":  pending,
		},
	}
	if err := conn.WriteJSON(ack); err != nil {
		slog.Warn("send hello_ack", "server_id", serverID, "error", err)
		return
	}
	_ = queries.DeletePendingCommands(db, serverID)
	slog.Info("sent hello_ack", "server_id", serverID, "pending", len(pending))
}

// QueueOrSendCommand sends to agent or enqueues if offline.
func QueueOrSendCommand(hub *Hub, db *sql.DB, serverID string, cmd map[string]interface{}) error {
	cmdID, _ := cmd["id"].(string)
	if cmdID == "" {
		cmdID = uuid.New().String()
		cmd["id"] = cmdID
	}
	cmdType, _ := cmd["type"].(string)
	payloadBytes, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	envelope := map[string]interface{}{
		"type":    "command",
		"payload": json.RawMessage(payloadBytes),
	}
	msgBytes, _ := json.Marshal(envelope)

	if hub.SendToAgent(serverID, msgBytes) {
		return nil
	}

	projectKey := ""
	if p, ok := cmd["payload"].(map[string]interface{}); ok {
		if v, ok := p["app_name"].(string); ok {
			projectKey = v
		} else if v, ok := p["project_name"].(string); ok {
			projectKey = v
		}
	}
	expiresAt, _ := cmd["expires_at"].(string)
	return queries.EnqueuePendingCommand(db, cmdID, serverID, cmdType, string(payloadBytes), projectKey, expiresAt)
}
