package handlers_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/yourplatform/control-plane/internal/api/handlers"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

func setupCustomDomainTest(t *testing.T) (*sql.DB, *chi.Mux) {
	t.Helper()
	db := setupTestDB(t)

	// Insert test server
	_, err := db.Exec(
		"INSERT INTO servers (id, user_id, name, agent_id, agent_secret_hash, status, ip_address) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"server-1", "user-1", "test-server", "agent-1", "hash1", "connected", "1.2.3.4",
	)
	if err != nil {
		t.Fatalf("insert test server: %v", err)
	}

	// Insert test deployment
	_, err = db.Exec(
		"INSERT INTO deployments (id, server_id, app_name, image, port, domain, status) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"deploy-1", "server-1", "my-app", "nginx:latest", 8080, "my-app.srv-server-.yourplatform.app", "running",
	)
	if err != nil {
		t.Fatalf("insert test deployment: %v", err)
	}

	hub := ws.NewHub()
	handler := &handlers.CustomDomain{DB: db, Hub: hub}

	r := chi.NewRouter()
	r.Post("/servers/{serverID}/deployments/{deploymentID}/domains", handler.AddDomain)
	r.Post("/servers/{serverID}/deployments/{deploymentID}/domains/{domainID}/verify", handler.VerifyDomain)
	r.Delete("/servers/{serverID}/deployments/{deploymentID}/domains/{domainID}", handler.RemoveDomain)

	return db, r
}

func TestAddDomain_Success(t *testing.T) {
	_, r := setupCustomDomainTest(t)

	body := map[string]string{"domain": "shop.example.com"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/servers/server-1/deployments/deploy-1/domains", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["domain"] != "shop.example.com" {
		t.Errorf("domain = %v, want shop.example.com", resp["domain"])
	}
	if resp["status"] != "pending" {
		t.Errorf("status = %v, want pending", resp["status"])
	}
	if resp["server_ip"] != "1.2.3.4" {
		t.Errorf("server_ip = %v, want 1.2.3.4", resp["server_ip"])
	}

	// Verify DNS instructions are present
	dnsInstructions, ok := resp["dns_instructions"].(map[string]interface{})
	if !ok {
		t.Fatal("dns_instructions should be a map")
	}
	if dnsInstructions["value"] != "1.2.3.4" {
		t.Errorf("dns_instructions.value = %v, want 1.2.3.4", dnsInstructions["value"])
	}
}

func TestAddDomain_MissingDomain(t *testing.T) {
	_, r := setupCustomDomainTest(t)

	body := map[string]string{}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/servers/server-1/deployments/deploy-1/domains", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddDomain_DeploymentNotFound(t *testing.T) {
	_, r := setupCustomDomainTest(t)

	body := map[string]string{"domain": "shop.example.com"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/servers/server-1/deployments/nonexistent/domains", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddDomain_DuplicateDomain(t *testing.T) {
	db, r := setupCustomDomainTest(t)

	// Insert existing custom domain for a different deployment
	_, err := db.Exec(
		"INSERT INTO deployments (id, server_id, app_name, image, port, domain, status) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"deploy-2", "server-1", "other-app", "nginx:latest", 9090, "other.srv-server-.yourplatform.app", "running",
	)
	if err != nil {
		t.Fatalf("insert second deployment: %v", err)
	}
	_, err = db.Exec(
		"INSERT INTO custom_domains (id, deployment_id, domain, status) VALUES (?, ?, ?, ?)",
		"cd-1", "deploy-2", "shop.example.com", "verified",
	)
	if err != nil {
		t.Fatalf("insert existing custom domain: %v", err)
	}

	body := map[string]string{"domain": "shop.example.com"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/servers/server-1/deployments/deploy-1/domains", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyDomain_NotFound(t *testing.T) {
	_, r := setupCustomDomainTest(t)

	req := httptest.NewRequest(http.MethodPost, "/servers/server-1/deployments/deploy-1/domains/nonexistent/verify", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRemoveDomain_Success(t *testing.T) {
	db, r := setupCustomDomainTest(t)

	// Insert a custom domain to remove
	_, err := db.Exec(
		"INSERT INTO custom_domains (id, deployment_id, domain, status) VALUES (?, ?, ?, ?)",
		"cd-remove", "deploy-1", "remove-me.example.com", "verified",
	)
	if err != nil {
		t.Fatalf("insert custom domain: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/servers/server-1/deployments/deploy-1/domains/cd-remove", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["status"] != "removed" {
		t.Errorf("status = %v, want removed", resp["status"])
	}
	if resp["domain"] != "remove-me.example.com" {
		t.Errorf("domain = %v, want remove-me.example.com", resp["domain"])
	}

	// Verify domain was actually deleted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM custom_domains WHERE id = ?", "cd-remove").Scan(&count)
	if err != nil {
		t.Fatalf("count custom domains: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 domains after delete, got %d", count)
	}
}

func TestRemoveDomain_NotFound(t *testing.T) {
	_, r := setupCustomDomainTest(t)

	req := httptest.NewRequest(http.MethodDelete, "/servers/server-1/deployments/deploy-1/domains/nonexistent", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddDomain_SameDomainSameDeployment(t *testing.T) {
	db, r := setupCustomDomainTest(t)

	// Insert custom domain for the same deployment (should be allowed)
	_, err := db.Exec(
		"INSERT INTO custom_domains (id, deployment_id, domain, status) VALUES (?, ?, ?, ?)",
		"cd-existing", "deploy-1", "shop.example.com", "verified",
	)
	if err != nil {
		t.Fatalf("insert existing custom domain: %v", err)
	}

	// Adding the same domain to the same deployment should fail with conflict
	// because the unique index on domain prevents duplicates
	body := map[string]string{"domain": "shop.example.com"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/servers/server-1/deployments/deploy-1/domains", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Should fail because domain is already in use (even by same deployment)
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate domain, got %d: %s", w.Code, w.Body.String())
	}
}
