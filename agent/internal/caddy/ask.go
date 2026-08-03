package caddy

import (
	"log/slog"
	"net/http"
	"sync"
)

// DomainAuthorizer checks if a domain is authorized for on-demand TLS.
type DomainAuthorizer struct {
	mu      sync.RWMutex
	domains map[string]bool // domain → authorized
}

// NewDomainAuthorizer creates a new domain authorizer.
func NewDomainAuthorizer() *DomainAuthorizer {
	return &DomainAuthorizer{
		domains: make(map[string]bool),
	}
}

// SetDomains replaces the entire set of authorized domains.
func (da *DomainAuthorizer) SetDomains(domains []string) {
	da.mu.Lock()
	defer da.mu.Unlock()
	da.domains = make(map[string]bool, len(domains))
	for _, d := range domains {
		da.domains[d] = true
	}
	slog.Info("domain authorizer updated", "count", len(domains), "domains", domains)
}

// AddDomain adds a single domain to the authorized set.
func (da *DomainAuthorizer) AddDomain(domain string) {
	da.mu.Lock()
	defer da.mu.Unlock()
	da.domains[domain] = true
}

// RemoveDomain removes a domain from the authorized set.
func (da *DomainAuthorizer) RemoveDomain(domain string) {
	da.mu.Lock()
	defer da.mu.Unlock()
	delete(da.domains, domain)
}

// IsAuthorized checks if a domain is authorized.
func (da *DomainAuthorizer) IsAuthorized(domain string) bool {
	da.mu.RLock()
	defer da.mu.RUnlock()
	return da.domains[domain]
}

// HandleAsk handles the on-demand TLS ask endpoint.
// Caddy calls: GET /__yourplatform_ask?domain=example.com
// Returns 200 if domain is authorized, 403 if not.
func (da *DomainAuthorizer) HandleAsk(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		http.Error(w, "missing domain parameter", http.StatusBadRequest)
		return
	}

	if da.IsAuthorized(domain) {
		w.WriteHeader(http.StatusOK)
		return
	}

	slog.Warn("on-demand TLS denied", "domain", domain)
	http.Error(w, "domain not authorized", http.StatusForbidden)
}
