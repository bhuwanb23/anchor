package queries

import (
	"database/sql"
	"time"
)

type CustomDomain struct {
	ID           string
	DeploymentID string
	Domain       string
	Status       string
	VerifiedAt   sql.NullString
	CreatedAt    string
	UpdatedAt    string
}

func InsertCustomDomain(db *sql.DB, id, deploymentID, domain string) error {
	_, err := db.Exec(
		"INSERT INTO custom_domains (id, deployment_id, domain) VALUES (?, ?, ?)",
		id, deploymentID, domain,
	)
	return err
}

func GetCustomDomainByDomain(db *sql.DB, domain string) (id, deploymentID, status string, err error) {
	err = db.QueryRow(
		"SELECT id, deployment_id, status FROM custom_domains WHERE domain = ?",
		domain,
	).Scan(&id, &deploymentID, &status)
	return
}

func GetCustomDomainsByDeployment(db *sql.DB, deploymentID string) ([]CustomDomain, error) {
	rows, err := db.Query(
		"SELECT id, deployment_id, domain, status, verified_at, created_at, updated_at FROM custom_domains WHERE deployment_id = ? ORDER BY created_at",
		deploymentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []CustomDomain
	for rows.Next() {
		var d CustomDomain
		if err := rows.Scan(&d.ID, &d.DeploymentID, &d.Domain, &d.Status, &d.VerifiedAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	if domains == nil {
		domains = []CustomDomain{}
	}
	return domains, rows.Err()
}

func UpdateCustomDomainStatus(db *sql.DB, domainID, status string) error {
	var verifiedAt sql.NullString
	if status == "verified" || status == "active" {
		verifiedAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true}
	}
	_, err := db.Exec(
		"UPDATE custom_domains SET status = ?, verified_at = ?, updated_at = ? WHERE id = ?",
		status, verifiedAt, time.Now().UTC().Format(time.RFC3339), domainID,
	)
	return err
}

func GetPendingCustomDomains(db *sql.DB) ([]CustomDomain, error) {
	rows, err := db.Query(
		"SELECT id, deployment_id, domain, status, verified_at, created_at, updated_at FROM custom_domains WHERE status = 'pending' ORDER BY created_at",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []CustomDomain
	for rows.Next() {
		var d CustomDomain
		if err := rows.Scan(&d.ID, &d.DeploymentID, &d.Domain, &d.Status, &d.VerifiedAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	if domains == nil {
		domains = []CustomDomain{}
	}
	return domains, rows.Err()
}

func DeleteCustomDomain(db *sql.DB, domainID string) error {
	_, err := db.Exec("DELETE FROM custom_domains WHERE id = ?", domainID)
	return err
}

func DeleteCustomDomainByDomain(db *sql.DB, domain string) error {
	_, err := db.Exec("DELETE FROM custom_domains WHERE domain = ?", domain)
	return err
}
