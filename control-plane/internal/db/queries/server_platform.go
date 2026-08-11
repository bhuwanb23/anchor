package queries

import (
	"database/sql"
	"encoding/json"
)

// ServerPlatform holds the full platform detection and readiness result.
type ServerPlatform struct {
	ServerID                string   `json:"server_id"`
	IsArm64                 bool     `json:"is_arm64"`
	CPUModelName            string   `json:"cpu_model_name"`
	CPUVendorID             string   `json:"cpu_vendor_id,omitempty"`
	CPUMicroarchitecture    string   `json:"cpu_microarchitecture,omitempty"`
	CPUPartCode             string   `json:"cpu_part_code,omitempty"`
	CPUCloudProviderHint    string   `json:"cpu_cloud_provider_hint,omitempty"`
	CPUDetectionConfidence  string   `json:"cpu_detection_confidence"`
	CPUCores                int      `json:"cpu_cores"`
	CPUMhz                  float64  `json:"cpu_mhz,omitempty"`
	FeatureDotprod          bool     `json:"feature_dotprod"`
	FeatureI8mm             bool     `json:"feature_i8mm"`
	FeatureSve              bool     `json:"feature_sve"`
	FeatureSve2             bool     `json:"feature_sve2"`
	FeatureBf16             bool     `json:"feature_bf16"`
	// Build selection
	ImageTag          string `json:"image_tag"`
	OptimizationLabel string `json:"optimization_label"`
	ExpectedHardware  string `json:"expected_hardware"`
	// Memory assessment
	MemoryTotalMB          int64   `json:"memory_total_mb"`
	MemoryAvailableMB      int64   `json:"memory_available_mb"`
	MemoryAvailableGB      float64 `json:"memory_available_gb"`
	MemoryRecommendedModel string  `json:"memory_recommended_model"`
	MemoryRecommendedQuant string  `json:"memory_recommended_quant"`
	MemorySufficient       bool    `json:"memory_sufficient"`
	MemoryNote             string  `json:"memory_note,omitempty"`
	// Disk assessment
	DiskTotalGB         float64 `json:"disk_total_gb"`
	DiskAvailableGB     float64 `json:"disk_available_gb"`
	DiskModelRequiredGB float64 `json:"disk_model_required_gb"`
	DiskSufficient      bool    `json:"disk_sufficient"`
	DiskNote            string  `json:"disk_note,omitempty"`
	// Readiness
	CanRunInference bool     `json:"can_run_inference"`
	BlockReason     string   `json:"block_reason,omitempty"`
	ReadinessNotes  []string `json:"readiness_notes,omitempty"`
	DetectedAt      string   `json:"detected_at"`
}

// UpsertServerPlatform inserts or replaces the full platform detection result.
func UpsertServerPlatform(db *sql.DB, p *ServerPlatform) error {
	notesJSON, _ := json.Marshal(p.ReadinessNotes)
	_, err := db.Exec(`
		INSERT INTO server_platform (
			server_id, is_arm64, cpu_model_name, cpu_vendor_id, cpu_microarchitecture,
			cpu_part_code, cpu_cloud_provider_hint, cpu_detection_confidence,
			cpu_cores, cpu_mhz, feature_dotprod, feature_i8mm, feature_sve,
			feature_sve2, feature_bf16, image_tag, optimization_label, expected_hardware,
			memory_total_mb, memory_available_mb, memory_available_gb,
			memory_recommended_model, memory_recommended_quant, memory_sufficient, memory_note,
			disk_total_gb, disk_available_gb, disk_model_required_gb, disk_sufficient, disk_note,
			can_run_inference, block_reason, readiness_notes, detected_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(server_id) DO UPDATE SET
			is_arm64=excluded.is_arm64, cpu_model_name=excluded.cpu_model_name,
			cpu_vendor_id=excluded.cpu_vendor_id, cpu_microarchitecture=excluded.cpu_microarchitecture,
			cpu_part_code=excluded.cpu_part_code, cpu_cloud_provider_hint=excluded.cpu_cloud_provider_hint,
			cpu_detection_confidence=excluded.cpu_detection_confidence,
			cpu_cores=excluded.cpu_cores, cpu_mhz=excluded.cpu_mhz,
			feature_dotprod=excluded.feature_dotprod, feature_i8mm=excluded.feature_i8mm,
			feature_sve=excluded.feature_sve, feature_sve2=excluded.feature_sve2,
			feature_bf16=excluded.feature_bf16,
			image_tag=excluded.image_tag, optimization_label=excluded.optimization_label,
			expected_hardware=excluded.expected_hardware,
			memory_total_mb=excluded.memory_total_mb, memory_available_mb=excluded.memory_available_mb,
			memory_available_gb=excluded.memory_available_gb,
			memory_recommended_model=excluded.memory_recommended_model,
			memory_recommended_quant=excluded.memory_recommended_quant,
			memory_sufficient=excluded.memory_sufficient, memory_note=excluded.memory_note,
			disk_total_gb=excluded.disk_total_gb, disk_available_gb=excluded.disk_available_gb,
			disk_model_required_gb=excluded.disk_model_required_gb,
			disk_sufficient=excluded.disk_sufficient, disk_note=excluded.disk_note,
			can_run_inference=excluded.can_run_inference, block_reason=excluded.block_reason,
			readiness_notes=excluded.readiness_notes,
			detected_at=datetime('now')`,
		p.ServerID, boolToInt(p.IsArm64), nullStr(p.CPUModelName), nullStr(p.CPUVendorID),
		nullStr(p.CPUMicroarchitecture), nullStr(p.CPUPartCode), nullStr(p.CPUCloudProviderHint),
		nullStr(p.CPUDetectionConfidence), p.CPUCores, p.CPUMhz,
		boolToInt(p.FeatureDotprod), boolToInt(p.FeatureI8mm), boolToInt(p.FeatureSve),
		boolToInt(p.FeatureSve2), boolToInt(p.FeatureBf16),
		nullStr(p.ImageTag), nullStr(p.OptimizationLabel), nullStr(p.ExpectedHardware),
		p.MemoryTotalMB, p.MemoryAvailableMB, p.MemoryAvailableGB,
		nullStr(p.MemoryRecommendedModel), nullStr(p.MemoryRecommendedQuant),
		boolToInt(p.MemorySufficient), nullStr(p.MemoryNote),
		p.DiskTotalGB, p.DiskAvailableGB, p.DiskModelRequiredGB,
		boolToInt(p.DiskSufficient), nullStr(p.DiskNote),
		boolToInt(p.CanRunInference), nullStr(p.BlockReason), string(notesJSON),
	)
	return err
}

// GetServerPlatform returns the platform info for a server, or nil if not found.
func GetServerPlatform(db *sql.DB, serverID string) (*ServerPlatform, error) {
	p := &ServerPlatform{}
	var cpuModel, cpuVendor, cpuMicro, cpuPart, cpuHint, cpuConf sql.NullString
	var imageTag, optLabel, expectedHw sql.NullString
	var memRecModel, memRecQuant, memNote sql.NullString
	var diskNote, blockReason, notesJSON sql.NullString
	err := db.QueryRow(`
		SELECT server_id, is_arm64, cpu_model_name, cpu_vendor_id, cpu_microarchitecture,
			cpu_part_code, cpu_cloud_provider_hint, cpu_detection_confidence,
			cpu_cores, cpu_mhz, feature_dotprod, feature_i8mm, feature_sve,
			feature_sve2, feature_bf16, image_tag, optimization_label, expected_hardware,
			memory_total_mb, memory_available_mb, memory_available_gb,
			memory_recommended_model, memory_recommended_quant, memory_sufficient, memory_note,
			disk_total_gb, disk_available_gb, disk_model_required_gb, disk_sufficient, disk_note,
			can_run_inference, block_reason, readiness_notes, detected_at
		FROM server_platform WHERE server_id = ?`, serverID,
	).Scan(
		&p.ServerID, &p.IsArm64, &cpuModel, &cpuVendor, &cpuMicro,
		&cpuPart, &cpuHint, &cpuConf, &p.CPUCores, &p.CPUMhz,
		&p.FeatureDotprod, &p.FeatureI8mm, &p.FeatureSve,
		&p.FeatureSve2, &p.FeatureBf16, &imageTag, &optLabel, &expectedHw,
		&p.MemoryTotalMB, &p.MemoryAvailableMB, &p.MemoryAvailableGB,
		&memRecModel, &memRecQuant, &p.MemorySufficient, &memNote,
		&p.DiskTotalGB, &p.DiskAvailableGB, &p.DiskModelRequiredGB, &p.DiskSufficient, &diskNote,
		&p.CanRunInference, &blockReason, &notesJSON, &p.DetectedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.CPUModelName = cpuModel.String
	p.CPUVendorID = cpuVendor.String
	p.CPUMicroarchitecture = cpuMicro.String
	p.CPUPartCode = cpuPart.String
	p.CPUCloudProviderHint = cpuHint.String
	p.CPUDetectionConfidence = cpuConf.String
	p.ImageTag = imageTag.String
	p.OptimizationLabel = optLabel.String
	p.ExpectedHardware = expectedHw.String
	p.MemoryRecommendedModel = memRecModel.String
	p.MemoryRecommendedQuant = memRecQuant.String
	p.MemoryNote = memNote.String
	p.DiskNote = diskNote.String
	p.BlockReason = blockReason.String
	if notesJSON.Valid && notesJSON.String != "" {
		_ = json.Unmarshal([]byte(notesJSON.String), &p.ReadinessNotes)
	}
	return p, nil
}

// boolToInt converts a bool to 0/1 for SQLite.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullStr returns a sql.NullString for a plain string.
func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
