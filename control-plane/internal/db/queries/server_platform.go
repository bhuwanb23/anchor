package queries

import (
	"database/sql"
)

// ServerPlatform holds the platform detection result for a server.
type ServerPlatform struct {
	ServerID                string  `json:"server_id"`
	IsArm64                 bool    `json:"is_arm64"`
	CPUModelName            string  `json:"cpu_model_name"`
	CPUVendorID             string  `json:"cpu_vendor_id,omitempty"`
	CPUMicroarchitecture    string  `json:"cpu_microarchitecture,omitempty"`
	CPUPartCode             string  `json:"cpu_part_code,omitempty"`
	CPUCloudProviderHint    string  `json:"cpu_cloud_provider_hint,omitempty"`
	CPUDetectionConfidence  string  `json:"cpu_detection_confidence"`
	CPUCores                int     `json:"cpu_cores"`
	CPUMhz                  float64 `json:"cpu_mhz,omitempty"`
	FeatureDotprod          bool    `json:"feature_dotprod"`
	FeatureI8mm             bool    `json:"feature_i8mm"`
	FeatureSve              bool    `json:"feature_sve"`
	FeatureSve2             bool    `json:"feature_sve2"`
	FeatureBf16             bool    `json:"feature_bf16"`
	MemoryTotalMB           int64   `json:"memory_total_mb"`
	MemoryAvailableMB       int64   `json:"memory_available_mb"`
	MemoryRecommendedModel  string  `json:"memory_recommended_model"`
	DiskTotalGB             float64 `json:"disk_total_gb"`
	DiskAvailableGB         float64 `json:"disk_available_gb"`
	RecommendedBuild        string  `json:"recommended_build"`
	RecommendedQuantization string  `json:"recommended_quantization"`
	DetectedAt              string  `json:"detected_at"`
}

// UpsertServerPlatform inserts or replaces the platform detection result.
func UpsertServerPlatform(db *sql.DB, p *ServerPlatform) error {
	_, err := db.Exec(`
		INSERT INTO server_platform (
			server_id, is_arm64, cpu_model_name, cpu_vendor_id, cpu_microarchitecture,
			cpu_part_code, cpu_cloud_provider_hint, cpu_detection_confidence,
			cpu_cores, cpu_mhz, feature_dotprod, feature_i8mm, feature_sve,
			feature_sve2, feature_bf16, memory_total_mb, memory_available_mb,
			memory_recommended_model, disk_total_gb, disk_available_gb,
			recommended_build, recommended_quantization, detected_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(server_id) DO UPDATE SET
			is_arm64=excluded.is_arm64, cpu_model_name=excluded.cpu_model_name,
			cpu_vendor_id=excluded.cpu_vendor_id, cpu_microarchitecture=excluded.cpu_microarchitecture,
			cpu_part_code=excluded.cpu_part_code, cpu_cloud_provider_hint=excluded.cpu_cloud_provider_hint,
			cpu_detection_confidence=excluded.cpu_detection_confidence,
			cpu_cores=excluded.cpu_cores, cpu_mhz=excluded.cpu_mhz,
			feature_dotprod=excluded.feature_dotprod, feature_i8mm=excluded.feature_i8mm,
			feature_sve=excluded.feature_sve, feature_sve2=excluded.feature_sve2,
			feature_bf16=excluded.feature_bf16, memory_total_mb=excluded.memory_total_mb,
			memory_available_mb=excluded.memory_available_mb,
			memory_recommended_model=excluded.memory_recommended_model,
			disk_total_gb=excluded.disk_total_gb, disk_available_gb=excluded.disk_available_gb,
			recommended_build=excluded.recommended_build,
			recommended_quantization=excluded.recommended_quantization,
			detected_at=datetime('now')`,
		p.ServerID, boolToInt(p.IsArm64), nullStr(p.CPUModelName), nullStr(p.CPUVendorID),
		nullStr(p.CPUMicroarchitecture), nullStr(p.CPUPartCode), nullStr(p.CPUCloudProviderHint),
		nullStr(p.CPUDetectionConfidence), p.CPUCores, p.CPUMhz,
		boolToInt(p.FeatureDotprod), boolToInt(p.FeatureI8mm), boolToInt(p.FeatureSve),
		boolToInt(p.FeatureSve2), boolToInt(p.FeatureBf16),
		p.MemoryTotalMB, p.MemoryAvailableMB, nullStr(p.MemoryRecommendedModel),
		p.DiskTotalGB, p.DiskAvailableGB,
		nullStr(p.RecommendedBuild), nullStr(p.RecommendedQuantization),
	)
	return err
}

// GetServerPlatform returns the platform info for a server, or nil if not found.
func GetServerPlatform(db *sql.DB, serverID string) (*ServerPlatform, error) {
	p := &ServerPlatform{}
	var cpuModel, cpuVendor, cpuMicro, cpuPart, cpuHint, cpuConf, memRec, recBuild, recQuant sql.NullString
	err := db.QueryRow(`
		SELECT server_id, is_arm64, cpu_model_name, cpu_vendor_id, cpu_microarchitecture,
			cpu_part_code, cpu_cloud_provider_hint, cpu_detection_confidence,
			cpu_cores, cpu_mhz, feature_dotprod, feature_i8mm, feature_sve,
			feature_sve2, feature_bf16, memory_total_mb, memory_available_mb,
			memory_recommended_model, disk_total_gb, disk_available_gb,
			recommended_build, recommended_quantization, detected_at
		FROM server_platform WHERE server_id = ?`, serverID,
	).Scan(
		&p.ServerID, &p.IsArm64, &cpuModel, &cpuVendor, &cpuMicro,
		&cpuPart, &cpuHint, &cpuConf, &p.CPUCores, &p.CPUMhz,
		&p.FeatureDotprod, &p.FeatureI8mm, &p.FeatureSve,
		&p.FeatureSve2, &p.FeatureBf16, &p.MemoryTotalMB, &p.MemoryAvailableMB,
		&memRec, &p.DiskTotalGB, &p.DiskAvailableGB,
		&recBuild, &recQuant, &p.DetectedAt,
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
	p.MemoryRecommendedModel = memRec.String
	p.RecommendedBuild = recBuild.String
	p.RecommendedQuantization = recQuant.String
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
