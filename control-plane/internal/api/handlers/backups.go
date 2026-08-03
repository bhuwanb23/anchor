package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

type Backup struct {
	DB  *sql.DB
	Hub *ws.Hub
}

// GetBackupConfig returns the backup configuration for a server.
// GET /api/v1/servers/{serverID}/backup/config
func (h *Backup) GetBackupConfig(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	config, err := queries.GetBackupConfigByServer(h.DB, serverID)
	if err == sql.ErrNoRows {
		// Create default config
		configID := uuid.New().String()
		if err := queries.InsertBackupConfig(h.DB, configID, serverID); err != nil {
			slog.Error("insert default backup config", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		config, err = queries.GetBackupConfigByServer(h.DB, serverID)
		if err != nil {
			slog.Error("get backup config after insert", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
	if err != nil {
		slog.Error("get backup config", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// UpdateBackupConfig updates the backup configuration for a server.
// PUT /api/v1/servers/{serverID}/backup/config
func (h *Backup) UpdateBackupConfig(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req struct {
		Enabled          bool    `json:"enabled"`
		Schedule         string  `json:"schedule"`
		RetentionDaily   int     `json:"retention_daily"`
		RetentionWeekly  int     `json:"retention_weekly"`
		RetentionMonthly int     `json:"retention_monthly"`
		S3Endpoint       *string `json:"s3_endpoint,omitempty"`
		S3AccessKey      *string `json:"s3_access_key,omitempty"`
		S3SecretKey      *string `json:"s3_secret_key,omitempty"`
		S3Bucket         *string `json:"s3_bucket,omitempty"`
		S3Region         *string `json:"s3_region,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Ensure config exists
	_, err := queries.GetBackupConfigByServer(h.DB, serverID)
	if err == sql.ErrNoRows {
		configID := uuid.New().String()
		if err := queries.InsertBackupConfig(h.DB, configID, serverID); err != nil {
			slog.Error("insert backup config", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	} else if err != nil {
		slog.Error("get backup config", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Update config
	if err := queries.UpdateBackupConfig(h.DB, serverID, req.Enabled, req.Schedule,
		req.RetentionDaily, req.RetentionWeekly, req.RetentionMonthly,
		req.S3Endpoint, req.S3AccessKey, req.S3SecretKey, req.S3Bucket, req.S3Region); err != nil {
		slog.Error("update backup config", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Send config to agent via WebSocket
	msg := map[string]interface{}{
		"type": "command",
		"payload": map[string]interface{}{
			"id":   uuid.New().String(),
			"type": "backup_config",
			"payload": map[string]interface{}{
				"enabled":           req.Enabled,
				"schedule":          req.Schedule,
				"retention_daily":   req.RetentionDaily,
				"retention_weekly":  req.RetentionWeekly,
				"retention_monthly": req.RetentionMonthly,
				"s3_endpoint":       req.S3Endpoint,
				"s3_access_key":     req.S3AccessKey,
				"s3_secret_key":     req.S3SecretKey,
				"s3_bucket":         req.S3Bucket,
				"s3_region":         req.S3Region,
			},
		},
	}

	msgBytes, _ := json.Marshal(msg)
	if !h.Hub.SendToAgent(serverID, msgBytes) {
		slog.Warn("agent not connected, backup config queued", "server_id", serverID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// GetBackupSnapshots returns recent backup snapshots for a server.
// GET /api/v1/servers/{serverID}/backup/snapshots
func (h *Backup) GetBackupSnapshots(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	snapshots, err := queries.GetBackupSnapshotsByServer(h.DB, serverID, 50)
	if err != nil {
		slog.Error("get backup snapshots", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snapshots)
}

// GetBackupJobs returns recent backup jobs for a server.
// GET /api/v1/servers/{serverID}/backup/jobs
func (h *Backup) GetBackupJobs(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	jobs, err := queries.GetBackupJobsByServer(h.DB, serverID, 20)
	if err != nil {
		slog.Error("get backup jobs", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

// TriggerBackup sends a backup command to the agent.
// POST /api/v1/servers/{serverID}/backup/trigger
func (h *Backup) TriggerBackup(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Paths) == 0 {
		req.Paths = []string{"/var/lib/yourplatform"}
	}

	// Create job record
	jobID := uuid.New().String()
	if err := queries.InsertBackupJob(h.DB, jobID, serverID); err != nil {
		slog.Error("insert backup job", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Send backup command to agent
	msg := map[string]interface{}{
		"type": "command",
		"payload": map[string]interface{}{
			"id":   jobID,
			"type": "backup_trigger",
			"payload": map[string]interface{}{
				"job_id": jobID,
				"paths":  req.Paths,
			},
		},
	}

	msgBytes, _ := json.Marshal(msg)
	if !h.Hub.SendToAgent(serverID, msgBytes) {
		slog.Warn("agent not connected, backup queued", "server_id", serverID)
		_ = queries.UpdateBackupJobStatus(h.DB, jobID, "failed", strPtr("agent not connected"), nil)
		http.Error(w, "agent not connected", http.StatusServiceUnavailable)
		return
	}

	_ = queries.UpdateBackupJobStatus(h.DB, jobID, "running", nil, nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"job_id": jobID,
		"status": "started",
	})
}

// BackupStatus handles backup status updates from the agent.
func (h *Backup) BackupStatus(serverID string, payload map[string]interface{}) {
	jobID, _ := payload["job_id"].(string)
	status, _ := payload["status"].(string)
	errorMsg, _ := payload["error"].(string)
	snapshotID, _ := payload["snapshot_id"].(string)

	if jobID == "" || status == "" {
		return
	}

	var errPtr, snapPtr *string
	if errorMsg != "" {
		errPtr = &errorMsg
	}
	if snapshotID != "" {
		snapPtr = &snapshotID
	}

	if err := queries.UpdateBackupJobStatus(h.DB, jobID, status, errPtr, snapPtr); err != nil {
		slog.Error("update backup job status", "job_id", jobID, "error", err)
	}
}

// GetBackupHistory returns paginated backup job history with full metadata.
// GET /api/v1/servers/{serverID}/backup/history
func (h *Backup) GetBackupHistory(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	limit := 20
	jobs, err := queries.GetBackupJobsByServer(h.DB, serverID, limit)
	if err != nil {
		slog.Error("get backup history", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

// GetBackupSchedule returns the backup schedule for a server.
// GET /api/v1/servers/{serverID}/backup/schedule
func (h *Backup) GetBackupSchedule(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	config, err := queries.GetBackupConfigWithSchedule(h.DB, serverID)
	if err == sql.ErrNoRows {
		// Create default config
		configID := uuid.New().String()
		if err := queries.InsertBackupConfig(h.DB, configID, serverID); err != nil {
			slog.Error("insert default backup config", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		config, err = queries.GetBackupConfigWithSchedule(h.DB, serverID)
		if err != nil {
			slog.Error("get backup config after insert", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
	if err != nil {
		slog.Error("get backup schedule", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// UpdateBackupSchedule updates the backup schedule hour.
// PUT /api/v1/servers/{serverID}/backup/schedule
func (h *Backup) UpdateBackupSchedule(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req struct {
		HourUTC int `json:"hour_utc"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.HourUTC < 0 || req.HourUTC > 23 {
		http.Error(w, "hour_utc must be 0-23", http.StatusBadRequest)
		return
	}

	// Ensure config exists
	_, err := queries.GetBackupConfigByServer(h.DB, serverID)
	if err == sql.ErrNoRows {
		configID := uuid.New().String()
		if err := queries.InsertBackupConfig(h.DB, configID, serverID); err != nil {
			slog.Error("insert backup config", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	if err := queries.UpdateBackupSchedule(h.DB, serverID, req.HourUTC); err != nil {
		slog.Error("update backup schedule", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Send updated config to agent
	schedule := fmt.Sprintf("%02d:00", req.HourUTC)
	msg := map[string]interface{}{
		"type": "command",
		"payload": map[string]interface{}{
			"id":   uuid.New().String(),
			"type": "backup_config",
			"payload": map[string]interface{}{
				"schedule": schedule,
			},
		},
	}

	msgBytes, _ := json.Marshal(msg)
	h.Hub.SendToAgent(serverID, msgBytes)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// GetBackupUsage returns storage usage stats for a server.
// GET /api/v1/servers/{serverID}/backup/usage
func (h *Backup) GetBackupUsage(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	totalBytes, snapshotCount, err := queries.GetBackupUsage(h.DB, serverID)
	if err != nil {
		slog.Error("get backup usage", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_bytes":    totalBytes,
		"snapshot_count": snapshotCount,
	})
}

func strPtr(s string) *string {
	return &s
}

// ---------------------------------------------------------------------------
// Restore handlers
// ---------------------------------------------------------------------------

// TriggerRestore sends a restore command to the agent.
// POST /api/v1/servers/{serverID}/backup/restore
func (h *Backup) TriggerRestore(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req struct {
		SnapshotID  string `json:"snapshot_id"`
		ProjectName string `json:"project_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.SnapshotID == "" || req.ProjectName == "" {
		http.Error(w, "snapshot_id and project_name are required", http.StatusBadRequest)
		return
	}

	// Create restore job record
	jobID := uuid.New().String()
	if err := queries.InsertRestoreJob(h.DB, jobID, serverID, req.SnapshotID, req.ProjectName); err != nil {
		slog.Error("insert restore job", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Send restore command to agent
	msg := map[string]interface{}{
		"type": "command",
		"payload": map[string]interface{}{
			"id":   jobID,
			"type": "backup_restore",
			"payload": map[string]interface{}{
				"job_id":        jobID,
				"snapshot_id":   req.SnapshotID,
				"project_name":  req.ProjectName,
				"server_id":     serverID,
			},
		},
	}

	msgBytes, _ := json.Marshal(msg)
	if !h.Hub.SendToAgent(serverID, msgBytes) {
		slog.Warn("agent not connected, restore queued", "server_id", serverID)
		_ = queries.UpdateRestoreJobStatus(h.DB, jobID, "failed", strPtr("agent not connected"))
		http.Error(w, "agent not connected", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"job_id": jobID,
		"status": "started",
	})
}

// RestoreStatus handles restore status updates from the agent.
func (h *Backup) RestoreStatus(serverID string, payload map[string]interface{}) {
	restoreID, _ := payload["restore_id"].(string)
	status, _ := payload["status"].(string)
	errorMsg, _ := payload["error"].(string)

	if restoreID == "" || status == "" {
		return
	}

	var errPtr *string
	if errorMsg != "" {
		errPtr = &errorMsg
	}

	if err := queries.UpdateRestoreJobStatus(h.DB, restoreID, status, errPtr); err != nil {
		slog.Error("update restore job status", "restore_id", restoreID, "error", err)
	}
}

// GetRestoreHistory returns restore job history for a server.
// GET /api/v1/servers/{serverID}/backup/restores
func (h *Backup) GetRestoreHistory(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	jobs, err := queries.GetRestoreJobsByServer(h.DB, serverID, 20)
	if err != nil {
		slog.Error("get restore history", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

// GetBackupVerificationStatus returns verification config and status for a server.
// GET /api/v1/servers/{serverID}/backup/verification
func (h *Backup) GetBackupVerificationStatus(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	config, err := queries.GetVerificationConfigByServer(h.DB, serverID)
	if err != nil {
		if err == sql.ErrNoRows {
			// Create default config
			id := fmt.Sprintf("vconf-%s", serverID)
			if err := queries.InsertVerificationConfig(h.DB, id, serverID); err != nil {
				slog.Error("create verification config", "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			config, err = queries.GetVerificationConfigByServer(h.DB, serverID)
			if err != nil {
				slog.Error("get verification config", "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
		} else {
			slog.Error("get verification config", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	// Get latest backup job with verification status
	jobs, err := queries.GetBackupJobsByServer(h.DB, serverID, 1)
	var lastVerification struct {
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	if err == nil && len(jobs) > 0 {
		lastVerification.Status = jobs[0].VerificationStatus.String
		lastVerification.Error = jobs[0].VerificationError.String
	}

	response := map[string]interface{}{
		"config": config,
		"last_verification": lastVerification,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// TriggerBackupVerification triggers a manual backup verification.
// POST /api/v1/servers/{serverID}/backup/verification/trigger
func (h *Backup) TriggerBackupVerification(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	// Send backup_verify command to agent
	msg := map[string]interface{}{
		"type": "backup_verify",
		"payload": map[string]interface{}{
			"server_id": serverID,
			"subset":    "5%", // Default to post-backup verification
		},
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		slog.Error("marshal verification message", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.Hub.SendToAgent(serverID, msgBytes)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "verification_triggered",
		"message": "Backup verification has been triggered",
	})
}
