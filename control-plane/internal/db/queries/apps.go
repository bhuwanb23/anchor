package queries

import (
	"database/sql"
)

// App represents the current state of an app on a server.
type App struct {
	ID                 string
	ServerID           string
	ProjectName        string
	Status             string
	CurrentImage       sql.NullString
	CurrentContainerID sql.NullString
	CurrentHostPort    sql.NullInt64
	PlatformDomain     sql.NullString
	CustomDomains      sql.NullString
	MemoryLimitMB      int
	CpuQuotaPercent    int
	AppPort            int
	CreatedAt          string
	UpdatedAt          string
}

// ProjectDatabase represents a database provisioned for a project.
type ProjectDatabase struct {
	ID          string
	AppID       string
	ServerID    string
	ProjectName string
	DbType      string
	DBVersion   sql.NullString
	DBName      sql.NullString
	Status      string
	CreatedAt   string
}

// EnvVarKey represents an environment variable key (not the value).
type EnvVarKey struct {
	ID        string
	AppID     string
	ServerID  string
	KeyName   string
	IsAuto    bool
	CreatedAt string
	UpdatedAt string
}

// InsertApp creates a new app record.
func InsertApp(db *sql.DB, id, serverID, projectName string) error {
	_, err := db.Exec(
		`INSERT INTO apps (id, server_id, project_name) VALUES (?, ?, ?)`,
		id, serverID, projectName,
	)
	return err
}

// GetApp retrieves an app by server ID and project name.
func GetApp(db *sql.DB, serverID, projectName string) (*App, error) {
	a := &App{}
	err := db.QueryRow(
		`SELECT id, server_id, project_name, status, current_image, current_container_id,
			current_host_port, platform_domain, custom_domains, memory_limit_mb,
			cpu_quota_percent, app_port, created_at, updated_at
		 FROM apps WHERE server_id = ? AND project_name = ?`,
		serverID, projectName,
	).Scan(
		&a.ID, &a.ServerID, &a.ProjectName, &a.Status, &a.CurrentImage,
		&a.CurrentContainerID, &a.CurrentHostPort, &a.PlatformDomain,
		&a.CustomDomains, &a.MemoryLimitMB, &a.CpuQuotaPercent, &a.AppPort,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// GetAppByID retrieves an app by its ID.
func GetAppByID(db *sql.DB, id string) (*App, error) {
	a := &App{}
	err := db.QueryRow(
		`SELECT id, server_id, project_name, status, current_image, current_container_id,
			current_host_port, platform_domain, custom_domains, memory_limit_mb,
			cpu_quota_percent, app_port, created_at, updated_at
		 FROM apps WHERE id = ?`,
		id,
	).Scan(
		&a.ID, &a.ServerID, &a.ProjectName, &a.Status, &a.CurrentImage,
		&a.CurrentContainerID, &a.CurrentHostPort, &a.PlatformDomain,
		&a.CustomDomains, &a.MemoryLimitMB, &a.CpuQuotaPercent, &a.AppPort,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// ListAppsByServer returns all apps for a server.
func ListAppsByServer(db *sql.DB, serverID string) ([]App, error) {
	rows, err := db.Query(
		`SELECT id, server_id, project_name, status, current_image, current_container_id,
			current_host_port, platform_domain, custom_domains, memory_limit_mb,
			cpu_quota_percent, app_port, created_at, updated_at
		 FROM apps WHERE server_id = ? ORDER BY project_name`,
		serverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []App
	for rows.Next() {
		var a App
		if err := rows.Scan(
			&a.ID, &a.ServerID, &a.ProjectName, &a.Status, &a.CurrentImage,
			&a.CurrentContainerID, &a.CurrentHostPort, &a.PlatformDomain,
			&a.CustomDomains, &a.MemoryLimitMB, &a.CpuQuotaPercent, &a.AppPort,
			&a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	if apps == nil {
		apps = []App{}
	}
	return apps, nil
}

// UpdateAppSettings updates memory, CPU quota, and app port for an app.
func UpdateAppSettings(db *sql.DB, appID string, memoryMB, cpuPercent, appPort int) error {
	_, err := db.Exec(
		`UPDATE apps SET memory_limit_mb = ?, cpu_quota_percent = ?, app_port = ?, updated_at = datetime('now') WHERE id = ?`,
		memoryMB, cpuPercent, appPort, appID,
	)
	return err
}

// UpdateAppStatus updates the status of an app.
func UpdateAppStatus(db *sql.DB, serverID, projectName, status string) error {
	_, err := db.Exec(
		`UPDATE apps SET status = ?, updated_at = datetime('now') WHERE server_id = ? AND project_name = ?`,
		status, serverID, projectName,
	)
	return err
}

// UpdateAppDeployment updates the current deployment info for an app.
func UpdateAppDeployment(db *sql.DB, serverID, projectName, image, containerID string, hostPort int) error {
	_, err := db.Exec(
		`UPDATE apps SET current_image = ?, current_container_id = ?, current_host_port = ?,
			status = 'running', updated_at = datetime('now')
		 WHERE server_id = ? AND project_name = ?`,
		image, containerID, hostPort, serverID, projectName,
	)
	return err
}

// UpsertApp inserts or updates an app based on server_id + project_name.
func UpsertApp(db *sql.DB, id, serverID, projectName, status, image string, hostPort, appPort int) error {
	_, err := db.Exec(
		`INSERT INTO apps (id, server_id, project_name, status, current_image, current_host_port, app_port)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (server_id, project_name) DO UPDATE SET
			status = excluded.status,
			current_image = excluded.current_image,
			current_host_port = excluded.current_host_port,
			app_port = excluded.app_port,
			updated_at = datetime('now')`,
		id, serverID, projectName, status, image, hostPort, appPort,
	)
	return err
}

// DeleteAppsByServer removes all apps for a server.
func DeleteAppsByServer(db *sql.DB, serverID string) error {
	_, err := db.Exec(`DELETE FROM apps WHERE server_id = ?`, serverID)
	return err
}

// --- Project Database queries ---

// InsertProjectDatabase creates a new project database record.
func InsertProjectDatabase(db *sql.DB, id, appID, serverID, projectName, dbType string) error {
	_, err := db.Exec(
		`INSERT INTO project_databases (id, app_id, server_id, project_name, db_type)
		 VALUES (?, ?, ?, ?, ?)`,
		id, appID, serverID, projectName, dbType,
	)
	return err
}

// ListProjectDatabasesByServer returns all project databases for a server.
func ListProjectDatabasesByServer(db *sql.DB, serverID string) ([]ProjectDatabase, error) {
	rows, err := db.Query(
		`SELECT id, app_id, server_id, project_name, db_type, db_version, db_name, status, created_at
		 FROM project_databases WHERE server_id = ? ORDER BY project_name`,
		serverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dbs []ProjectDatabase
	for rows.Next() {
		var d ProjectDatabase
		if err := rows.Scan(
			&d.ID, &d.AppID, &d.ServerID, &d.ProjectName, &d.DbType,
			&d.DBVersion, &d.DBName, &d.Status, &d.CreatedAt,
		); err != nil {
			return nil, err
		}
		dbs = append(dbs, d)
	}
	if dbs == nil {
		dbs = []ProjectDatabase{}
	}
	return dbs, nil
}

// --- Env Var Key queries ---

// InsertEnvVarKey creates a new env var key.
func InsertEnvVarKey(db *sql.DB, id, appID, serverID, keyName string, isAuto bool) error {
	auto := 0
	if isAuto {
		auto = 1
	}
	_, err := db.Exec(
		`INSERT INTO env_var_keys (id, app_id, server_id, key_name, is_auto)
		 VALUES (?, ?, ?, ?, ?)`,
		id, appID, serverID, keyName, auto,
	)
	return err
}

// ListEnvVarKeysByApp returns all env var keys for an app.
func ListEnvVarKeysByApp(db *sql.DB, appID string) ([]EnvVarKey, error) {
	rows, err := db.Query(
		`SELECT id, app_id, server_id, key_name, is_auto, created_at, updated_at
		 FROM env_var_keys WHERE app_id = ? ORDER BY key_name`,
		appID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []EnvVarKey
	for rows.Next() {
		var k EnvVarKey
		var auto int
		if err := rows.Scan(
			&k.ID, &k.AppID, &k.ServerID, &k.KeyName, &auto,
			&k.CreatedAt, &k.UpdatedAt,
		); err != nil {
			return nil, err
		}
		k.IsAuto = auto == 1
		keys = append(keys, k)
	}
	if keys == nil {
		keys = []EnvVarKey{}
	}
	return keys, nil
}

// DeleteEnvVarKey removes an env var key.
func DeleteEnvVarKey(db *sql.DB, id string) error {
	_, err := db.Exec(`DELETE FROM env_var_keys WHERE id = ?`, id)
	return err
}
