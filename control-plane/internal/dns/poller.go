package dns

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

const (
	defaultInterval = 5 * time.Minute
	defaultTimeout  = 2 * time.Hour
)

// Poller retries DNS verification for pending custom domains.
type Poller struct {
	db       *sql.DB
	hub      *ws.Hub
	interval time.Duration
	timeout  time.Duration
}

// NewPoller creates a DNS verification poller.
func NewPoller(db *sql.DB, hub *ws.Hub) *Poller {
	return &Poller{
		db:       db,
		hub:      hub,
		interval: defaultInterval,
		timeout:  defaultTimeout,
	}
}

// Run starts the polling loop. It checks pending domains every interval
// and marks domains as failed after timeout.
func (p *Poller) Run(ctx context.Context) {
	slog.Info("dns verification poller started", "interval", p.interval, "timeout", p.timeout)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("dns verification poller stopped")
			return
		case <-ticker.C:
			p.checkPending(ctx)
		}
	}
}

func (p *Poller) checkPending(ctx context.Context) {
	domains, err := queries.GetPendingCustomDomains(p.db)
	if err != nil {
		slog.Error("poller: get pending domains", "error", err)
		return
	}

	for _, cd := range domains {
		// Check if domain has been pending too long
		createdAt, err := time.Parse(time.RFC3339, cd.CreatedAt)
		if err == nil && time.Since(createdAt) > p.timeout {
			slog.Info("poller: domain timed out", "domain", cd.Domain, "created", cd.CreatedAt)
			_ = queries.UpdateCustomDomainStatus(p.db, cd.ID, "failed")
			continue
		}

		// Get the deployment's server IP
		serverID, serverIP, err := p.getDeploymentServerIP(cd.DeploymentID)
		if err != nil {
			slog.Warn("poller: could not get server IP", "domain", cd.Domain, "error", err)
			continue
		}

		// Verify DNS
		resolvedIP, ok, err := VerifyDomainResolves(ctx, cd.Domain, serverIP)
		if err != nil {
			slog.Warn("poller: dns verification error", "domain", cd.Domain, "error", err)
			continue
		}

		if ok {
			slog.Info("poller: domain verified", "domain", cd.Domain, "ip", resolvedIP)
			_ = queries.UpdateCustomDomainStatus(p.db, cd.ID, "verified")

			// Send domain update to agent
			if err := sendDomainUpdate(p.db, p.hub, cd.DeploymentID, serverID); err != nil {
				slog.Error("poller: send domain update", "domain", cd.Domain, "error", err)
			}
		} else {
			slog.Debug("poller: domain not yet verified", "domain", cd.Domain, "resolved", resolvedIP, "expected", serverIP)
		}
	}
}

func (p *Poller) getDeploymentServerIP(deploymentID string) (serverID, serverIP string, err error) {
	err = p.db.QueryRow(
		"SELECT d.server_id, s.ip_address FROM deployments d JOIN servers s ON d.server_id = s.id WHERE d.id = ?",
		deploymentID,
	).Scan(&serverID, &serverIP)
	return
}

// sendDomainUpdate sends the complete domain list for a deployment to the agent.
func sendDomainUpdate(db *sql.DB, hub *ws.Hub, deploymentID, serverID string) error {
	var appName, platformDomain string
	err := db.QueryRow(
		"SELECT app_name, domain FROM deployments WHERE id = ?",
		deploymentID,
	).Scan(&appName, &platformDomain)
	if err != nil {
		return err
	}

	domains := []string{}
	if platformDomain != "" {
		domains = append(domains, platformDomain)
	}

	customDomains, err := queries.GetCustomDomainsByDeployment(db, deploymentID)
	if err != nil {
		return err
	}
	for _, cd := range customDomains {
		if cd.Status == "verified" || cd.Status == "active" {
			domains = append(domains, cd.Domain)
		}
	}

	if len(domains) == 0 {
		return nil
	}

	msg := map[string]interface{}{
		"type":       "command",
		"payload": map[string]interface{}{
			"id":   deploymentID,
			"type": "update_domains",
			"payload": map[string]interface{}{
				"app_name": appName,
				"domains":  domains,
			},
		},
	}

	msgBytes, _ := json.Marshal(msg)
	if !hub.SendToAgent(serverID, msgBytes) {
		slog.Warn("poller: agent not connected", "server_id", serverID)
	}

	return nil
}
