package handlers

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

// resolveAppDeployment finds the app and its newest deployment (for domain ops).
func resolveAppDeployment(db *sql.DB, serverID, appID string) (*queries.App, *queries.Deployment, error) {
	app, err := queries.GetAppByID(db, appID)
	if err != nil || app == nil || app.ServerID != serverID {
		return nil, nil, sql.ErrNoRows
	}
	deps, err := queries.ListDeploymentsByApp(db, serverID, app.ProjectName, 1)
	if err != nil {
		return app, nil, err
	}
	if len(deps) == 0 {
		return app, nil, nil
	}
	return app, &deps[0], nil
}

func requireServerAccess(db *sql.DB, w http.ResponseWriter, r *http.Request, serverID string) bool {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return false
	}
	role, _ := queries.GetUserServerRole(db, userID, serverID)
	if role == "" {
		Respond403(w, r, "You do not have access to this server")
		return false
	}
	return true
}

func withDeploymentParams(r *http.Request, serverID, deploymentID, domainID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("serverID", serverID)
	rctx.URLParams.Add("deploymentID", deploymentID)
	if domainID != "" {
		rctx.URLParams.Add("domainID", domainID)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// ListAppDomains GET /apps/{appID}/domains
func (h *CustomDomain) ListAppDomains(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	if !requireServerAccess(h.DB, w, r, serverID) {
		return
	}

	_, dep, err := resolveAppDeployment(h.DB, serverID, appID)
	if err == sql.ErrNoRows {
		Respond404(w, r, "App")
		return
	}
	if err != nil {
		Respond500(w, r)
		return
	}

	var domains []queries.CustomDomain
	if dep != nil {
		domains, err = queries.GetCustomDomainsByDeployment(h.DB, dep.ID)
		if err != nil {
			Respond500(w, r)
			return
		}
	}
	if domains == nil {
		domains = []queries.CustomDomain{}
	}

	out := make([]map[string]interface{}, 0, len(domains))
	for _, d := range domains {
		item := map[string]interface{}{
			"id":            d.ID,
			"domain":        d.Domain,
			"status":        d.Status,
			"deployment_id": d.DeploymentID,
			"created_at":    d.CreatedAt,
		}
		if d.VerifiedAt.Valid {
			item["verified_at"] = d.VerifiedAt.String
		}
		out = append(out, item)
	}
	RespondJSON(w, http.StatusOK, map[string]interface{}{"domains": out})
}

// AddAppDomain POST /apps/{appID}/domains
func (h *CustomDomain) AddAppDomain(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	if !requireServerAccess(h.DB, w, r, serverID) {
		return
	}

	_, dep, err := resolveAppDeployment(h.DB, serverID, appID)
	if err == sql.ErrNoRows {
		Respond404(w, r, "App")
		return
	}
	if err != nil {
		Respond500(w, r)
		return
	}
	if dep == nil {
		Respond400(w, r, "Deploy the app before adding a custom domain")
		return
	}

	h.AddDomain(w, withDeploymentParams(r, serverID, dep.ID, ""))
}

// VerifyAppDomain POST /apps/{appID}/domains/{domain}/verify
func (h *CustomDomain) VerifyAppDomain(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	domainOrID := chi.URLParam(r, "domain")
	if !requireServerAccess(h.DB, w, r, serverID) {
		return
	}

	_, dep, err := resolveAppDeployment(h.DB, serverID, appID)
	if err == sql.ErrNoRows || dep == nil {
		Respond404(w, r, "App")
		return
	}

	domainID := domainOrID
	if id, _, _, err := queries.GetCustomDomainByDomain(h.DB, domainOrID); err == nil {
		domainID = id
	}

	h.VerifyDomain(w, withDeploymentParams(r, serverID, dep.ID, domainID))
}

// RemoveAppDomain DELETE /apps/{appID}/domains/{domain}
func (h *CustomDomain) RemoveAppDomain(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	domainOrID := chi.URLParam(r, "domain")
	if !requireServerAccess(h.DB, w, r, serverID) {
		return
	}

	_, dep, err := resolveAppDeployment(h.DB, serverID, appID)
	if err == sql.ErrNoRows || dep == nil {
		Respond404(w, r, "App")
		return
	}

	domainID := domainOrID
	if id, _, _, err := queries.GetCustomDomainByDomain(h.DB, domainOrID); err == nil {
		domainID = id
	}

	h.RemoveDomain(w, withDeploymentParams(r, serverID, dep.ID, domainID))
}
