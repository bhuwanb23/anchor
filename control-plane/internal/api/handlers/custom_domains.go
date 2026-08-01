package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	"github.com/yourname/yourplatform/control-plane/internal/dns"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

type CustomDomain struct {
	DB   *sql.DB
	Hub  *ws.Hub
}

// AddDomain adds a custom domain to a deployment.
// POST /api/v1/servers/{serverID}/deployments/{deploymentID}/domains
func (h *CustomDomain) AddDomain(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	deploymentID := chi.URLParam(r, "deploymentID")

	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Domain == "" {
		http.Error(w, "domain is required", http.StatusBadRequest)
		return
	}

	// Verify deployment exists and belongs to this server
	var existingDomain string
	err := h.DB.QueryRow(
		"SELECT COALESCE(domain, '') FROM deployments WHERE id = ? AND server_id = ?",
		deploymentID, serverID,
	).Scan(&existingDomain)
	if err == sql.ErrNoRows {
		http.Error(w, "deployment not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("query deployment", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Check if domain is already claimed by another deployment
	_, existingDeploymentID, _, err := queries.GetCustomDomainByDomain(h.DB, req.Domain)
	if err == nil && existingDeploymentID != deploymentID {
		http.Error(w, "domain is already claimed by another deployment", http.StatusConflict)
		return
	}
	if err != nil && err != sql.ErrNoRows {
		slog.Error("check domain uniqueness", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Insert custom domain
	domainID := uuid.New().String()
	if err := queries.InsertCustomDomain(h.DB, domainID, deploymentID, req.Domain); err != nil {
		slog.Error("insert custom domain", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Get server IP for DNS instructions
	var serverIP string
	err = h.DB.QueryRow("SELECT ip_address FROM servers WHERE id = ?", serverID).Scan(&serverIP)
	if err != nil {
		slog.Error("get server IP", "error", err)
		serverIP = "unknown"
	}

	// Trigger immediate verification attempt in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resolvedIP, ok, err := dns.VerifyDomainResolves(ctx, req.Domain, serverIP)
		if err != nil {
			slog.Debug("initial dns verification failed", "domain", req.Domain, "error", err)
			return
		}
		if ok {
			slog.Info("domain verified on first check", "domain", req.Domain)
			_ = queries.UpdateCustomDomainStatus(h.DB, domainID, "verified")
			_ = sendDomainUpdate(h.DB, h.Hub, deploymentID, serverID)
		} else {
			slog.Debug("domain not yet verified", "domain", req.Domain, "resolved", resolvedIP, "expected", serverIP)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":             domainID,
		"domain":         req.Domain,
		"status":         "pending",
		"server_ip":      serverIP,
		"dns_instructions": map[string]string{
			"type":    "A Record",
			"name":    req.Domain,
			"value":   serverIP,
			"ttl":     "3600",
			"message": "Point your domain's A record to this IP address. DNS changes may take up to 48 hours to propagate.",
		},
	})
}

// VerifyDomain triggers DNS verification for a custom domain.
// POST /api/v1/servers/{serverID}/deployments/{deploymentID}/domains/{domainID}/verify
func (h *CustomDomain) VerifyDomain(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	deploymentID := chi.URLParam(r, "deploymentID")
	domainID := chi.URLParam(r, "domainID")

	// Verify the custom domain belongs to this deployment
	var domain, status string
	err := h.DB.QueryRow(
		"SELECT domain, status FROM custom_domains WHERE id = ? AND deployment_id = ?",
		domainID, deploymentID,
	).Scan(&domain, &status)
	if err == sql.ErrNoRows {
		http.Error(w, "custom domain not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("query custom domain", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Get server IP
	var serverIP string
	err = h.DB.QueryRow("SELECT ip_address FROM servers WHERE id = ?", serverID).Scan(&serverIP)
	if err != nil {
		slog.Error("get server IP", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Verify DNS
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	resolvedIP, ok, err := dns.VerifyDomainResolves(ctx, domain, serverIP)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"verified":  false,
			"error":     err.Error(),
			"server_ip": serverIP,
		})
		return
	}

	if ok {
		_ = queries.UpdateCustomDomainStatus(h.DB, domainID, "verified")
		_ = sendDomainUpdate(h.DB, h.Hub, deploymentID, serverID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"verified": true,
			"domain":   domain,
			"ip":       resolvedIP,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"verified":  false,
		"domain":    domain,
		"current_ip": resolvedIP,
		"expected_ip": serverIP,
		"message":   "DNS not yet propagated. Changes can take up to 48 hours. Will retry every 5 minutes.",
	})
}

// RemoveDomain removes a custom domain from a deployment.
// DELETE /api/v1/servers/{serverID}/deployments/{deploymentID}/domains/{domainID}
func (h *CustomDomain) RemoveDomain(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	deploymentID := chi.URLParam(r, "deploymentID")
	domainID := chi.URLParam(r, "domainID")

	// Verify the custom domain belongs to this deployment
	var domain string
	err := h.DB.QueryRow(
		"SELECT domain FROM custom_domains WHERE id = ? AND deployment_id = ?",
		domainID, deploymentID,
	).Scan(&domain)
	if err == sql.ErrNoRows {
		http.Error(w, "custom domain not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("query custom domain", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Delete the custom domain
	if err := queries.DeleteCustomDomain(h.DB, domainID); err != nil {
		slog.Error("delete custom domain", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Send updated domain list to agent
	_ = sendDomainUpdate(h.DB, h.Hub, deploymentID, serverID)

	slog.Info("custom domain removed", "domain", domain, "deployment_id", deploymentID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "removed",
		"domain": domain,
	})
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
		slog.Warn("agent not connected, domain update queued", "server_id", serverID)
	}

	return nil
}
