package executor

import (
	"encoding/json"
	"time"
)

// Extended Command envelope (Layer 4B Step 1).
// Existing fields remain; new fields are optional for backward compatibility.
type CommandEnvelope struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	ServerID  string          `json:"server_id,omitempty"`
	IssuedBy  string          `json:"issued_by,omitempty"`
	IssuedAt  string          `json:"issued_at,omitempty"`
	ExpiresAt string          `json:"expires_at,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

// ToCommand converts envelope to the internal Command type.
func (e CommandEnvelope) ToCommand() Command {
	return Command{
		ID:        e.ID,
		Type:      e.Type,
		ServerID:  e.ServerID,
		IssuedBy:  e.IssuedBy,
		IssuedAt:  e.IssuedAt,
		ExpiresAt: e.ExpiresAt,
		Payload:   e.Payload,
	}
}

// IsExpired returns true if expires_at is set and in the past.
func (e CommandEnvelope) IsExpired(now time.Time) bool {
	if e.ExpiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, e.ExpiresAt)
	if err != nil {
		return false
	}
	return now.After(t)
}

// ProgressSender sends command_progress messages.
type ProgressSender interface {
	SendJSON(v interface{}) error
}

// SendProgress emits a command_progress event.
func SendProgress(sender ProgressSender, commandID, phase, message string, percent int) {
	if sender == nil {
		return
	}
	_ = sender.SendJSON(map[string]interface{}{
		"type": "command_progress",
		"payload": map[string]interface{}{
			"command_id": commandID,
			"phase":      phase,
			"message":    message,
			"percent":    percent,
		},
	})
}

// SendAck emits command_ack immediately after receipt.
func SendAck(sender ProgressSender, commandID string) {
	if sender == nil {
		return
	}
	_ = sender.SendJSON(map[string]interface{}{
		"type": "command_ack",
		"payload": map[string]string{
			"command_id": commandID,
			"status":     "acknowledged",
		},
	})
}

// ProjectKeyFromCommand extracts the project name for slot assignment.
func ProjectKeyFromCommand(cmd Command) string {
	if len(cmd.Payload) == 0 {
		return projectKeyForType(cmd.Type)
	}
	var meta struct {
		AppName     string `json:"app_name"`
		ProjectName string `json:"project_name"`
		Name        string `json:"name"`
	}
	_ = json.Unmarshal(cmd.Payload, &meta)
	if meta.AppName != "" {
		return meta.AppName
	}
	if meta.ProjectName != "" {
		return meta.ProjectName
	}
	if meta.Name != "" {
		return meta.Name
	}
	return projectKeyForType(cmd.Type)
}

func projectKeyForType(cmdType string) string {
	switch cmdType {
	case "run_preflight", "get_state", "update_agent", "preflight", "detect_platform", "deploy_inference", "run_benchmark":
		return "global"
	case "start_log_stream", "stop_log_stream":
		return "" // run outside slots (caller handles)
	default:
		return "global"
	}
}
