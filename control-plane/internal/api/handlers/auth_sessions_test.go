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
	appmiddleware "github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
)

// sessionsRouter builds a chi router with the Step 4 endpoints behind the real
// Auth middleware, using the same "test-secret" as newAuthHandler, so tests
// exercise the full protected path (token → user → claims → handler).
func sessionsRouter(db *sql.DB) http.Handler {
	r := chi.NewRouter()
	r.Use(appmiddleware.Auth(db, "test-secret"))
	h := newAuthHandler(db)
	r.Post("/auth/logout", h.Logout)
	r.Post("/auth/logout-all", h.LogoutAll)
	r.Get("/auth/sessions", h.Sessions)
	r.Delete("/auth/sessions/{sessionID}", h.DeleteSession)
	return r
}

// serveAuthenticated runs a request against the sessions router with a Bearer
// access token attached.
func serveAuthenticated(router http.Handler, method, path, accessToken string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// logoutLoginHelper logs in a fresh user and returns the login response. It is
// a thin alias for the shared loginHelper in auth_refresh_test.go.
func logoutLoginHelper(t *testing.T, h *handlers.Auth) map[string]interface{} {
	t.Helper()
	return loginHelper(t, h)
}

// twoSessionsForSameUser registers one user and logs in twice, returning both
// login responses. Unlike loginHelper (which makes a NEW user per call), this
// produces two independent sessions for the SAME account — the realistic
// "logged in on two devices" scenario the Step 4 endpoints are built for.
func twoSessionsForSameUser(t *testing.T, h *handlers.Auth) (map[string]interface{}, map[string]interface{}) {
	t.Helper()
	email := "multi" + uniqueSuffix() + "@example.com"
	password := "correct horse battery staple"
	if w := registerRequest(t, h, map[string]string{
		"name":     "Multi Device",
		"email":    email,
		"password": password,
	}); w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", w.Code)
	}

	login := func() map[string]interface{} {
		body, _ := json.Marshal(map[string]string{"email": email, "password": password})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Login(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("login: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&resp)
		return resp
	}

	return login(), login()
}

func TestLogout_RevokesRefreshToken(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)

	loginResp := logoutLoginHelper(t, newAuthHandler(db))
	accessToken := loginResp["access_token"].(string)
	refreshToken := loginResp["refresh_token"].(string)

	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	w := serveAuthenticated(router, http.MethodPost, "/auth/logout", accessToken, string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The revoked refresh token can no longer mint access tokens.
	if w := refreshHelper(newAuthHandler(db), refreshToken); w.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout: expected 401, got %d", w.Code)
	}
}

func TestLogout_InvalidToken(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)

	loginResp := logoutLoginHelper(t, newAuthHandler(db))
	accessToken := loginResp["access_token"].(string)

	body, _ := json.Marshal(map[string]string{"refresh_token": "rt_does-not-exist"})
	w := serveAuthenticated(router, http.MethodPost, "/auth/logout", accessToken, string(body))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("logout with unknown token: expected 401, got %d", w.Code)
	}
}

func TestLogout_CrossUserCannotRevoke(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)
	h := newAuthHandler(db)

	// Alice's session.
	aliceLogin := logoutLoginHelper(t, h)
	aliceRefresh := aliceLogin["refresh_token"].(string)

	// Bob logs in and tries to revoke Alice's session with his own access token.
	bobLogin := logoutLoginHelper(t, h)
	bobAccess := bobLogin["access_token"].(string)

	body, _ := json.Marshal(map[string]string{"refresh_token": aliceRefresh})
	w := serveAuthenticated(router, http.MethodPost, "/auth/logout", bobAccess, string(body))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("cross-user logout: expected 401, got %d", w.Code)
	}

	// Alice's session must still be live.
	if w := refreshHelper(h, aliceRefresh); w.Code != http.StatusOK {
		t.Errorf("alice refresh after cross-user logout attempt: expected 200, got %d", w.Code)
	}
}

func TestLogout_MissingBody(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)

	loginResp := logoutLoginHelper(t, newAuthHandler(db))
	accessToken := loginResp["access_token"].(string)

	w := serveAuthenticated(router, http.MethodPost, "/auth/logout", accessToken, `{}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("logout without refresh_token: expected 400, got %d", w.Code)
	}
}

func TestLogout_RequiresAccessToken(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)

	body, _ := json.Marshal(map[string]string{"refresh_token": "rt_whatever"})
	w := serveAuthenticated(router, http.MethodPost, "/auth/logout", "", string(body))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("logout without access token: expected 401, got %d", w.Code)
	}
}

func TestLogoutAll_RevokesAllSessions(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)
	h := newAuthHandler(db)

	// Two devices, same account.
	login1, login2 := twoSessionsForSameUser(t, h)

	w := serveAuthenticated(router, http.MethodPost, "/auth/logout-all", login1["access_token"].(string), "")
	if w.Code != http.StatusOK {
		t.Fatalf("logout-all: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["message"] != "Logged out from all devices" {
		t.Errorf("message = %v, want Logged out from all devices", resp["message"])
	}

	// Neither session can refresh anymore.
	if w := refreshHelper(h, login1["refresh_token"].(string)); w.Code != http.StatusUnauthorized {
		t.Errorf("session 1 refresh after logout-all: expected 401, got %d", w.Code)
	}
	if w := refreshHelper(h, login2["refresh_token"].(string)); w.Code != http.StatusUnauthorized {
		t.Errorf("session 2 refresh after logout-all: expected 401, got %d", w.Code)
	}
}

func TestSessions_ListActiveSessions(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)
	h := newAuthHandler(db)

	loginResp := logoutLoginHelper(t, h)
	accessToken := loginResp["access_token"].(string)

	w := serveAuthenticated(router, http.MethodGet, "/auth/sessions", accessToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("sessions: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var sessions []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions count = %d, want 1", len(sessions))
	}
	s := sessions[0]
	if s["current"] != true {
		t.Errorf("current = %v, want true for the requesting session", s["current"])
	}
	if s["id"] == "" || s["expires_at"] == "" || s["created_at"] == "" {
		t.Errorf("session missing id/expires_at/created_at: %v", s)
	}
}

func TestSessions_CurrentMarkedAcrossMultipleSessions(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)
	h := newAuthHandler(db)

	// Two sessions for the same user; request with the FIRST one's token.
	login1, _ := twoSessionsForSameUser(t, h)

	w := serveAuthenticated(router, http.MethodGet, "/auth/sessions", login1["access_token"].(string), "")
	if w.Code != http.StatusOK {
		t.Fatalf("sessions: expected 200, got %d", w.Code)
	}

	var sessions []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &sessions)
	if len(sessions) != 2 {
		t.Fatalf("sessions count = %d, want 2", len(sessions))
	}

	currentCount := 0
	for _, s := range sessions {
		if s["current"] == true {
			currentCount++
		}
	}
	if currentCount != 1 {
		t.Errorf("current sessions = %d, want exactly 1", currentCount)
	}
}

func TestSessions_ExcludesRevoked(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)
	h := newAuthHandler(db)

	loginResp := logoutLoginHelper(t, h)
	accessToken := loginResp["access_token"].(string)
	refreshToken := loginResp["refresh_token"].(string)

	// Revoke the session directly in the DB (simulating logout on another path).
	if _, err := db.Exec(
		"UPDATE refresh_tokens SET revoked_at = datetime('now') WHERE token_hash = ?",
		auth.HashRefreshToken(refreshToken),
	); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	w := serveAuthenticated(router, http.MethodGet, "/auth/sessions", accessToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("sessions: expected 200, got %d", w.Code)
	}
	var sessions []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &sessions)
	if len(sessions) != 0 {
		t.Errorf("sessions after revoke = %d, want 0", len(sessions))
	}
}

func TestDeleteSession_RevokesSpecificSession(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)
	h := newAuthHandler(db)

	login1, login2 := twoSessionsForSameUser(t, h)

	// Find the session IDs from the DB.
	var id1, id2 string
	if err := db.QueryRow(
		"SELECT id FROM refresh_tokens WHERE token_hash = ?",
		auth.HashRefreshToken(login1["refresh_token"].(string)),
	).Scan(&id1); err != nil {
		t.Fatalf("lookup session 1: %v", err)
	}
	if err := db.QueryRow(
		"SELECT id FROM refresh_tokens WHERE token_hash = ?",
		auth.HashRefreshToken(login2["refresh_token"].(string)),
	).Scan(&id2); err != nil {
		t.Fatalf("lookup session 2: %v", err)
	}

	// Using session 1's access token, revoke session 2.
	w := serveAuthenticated(router, http.MethodDelete, "/auth/sessions/"+id2, login1["access_token"].(string), "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete session: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Session 2 is dead; session 1 is untouched.
	if w := refreshHelper(h, login2["refresh_token"].(string)); w.Code != http.StatusUnauthorized {
		t.Errorf("deleted session refresh: expected 401, got %d", w.Code)
	}
	if w := refreshHelper(h, login1["refresh_token"].(string)); w.Code != http.StatusOK {
		t.Errorf("other session refresh: expected 200, got %d", w.Code)
	}
}

func TestDeleteSession_CanRevokeCurrentSession(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)
	h := newAuthHandler(db)

	login1, login2 := twoSessionsForSameUser(t, h)

	// Find the CURRENT session (the one tied to login1's access token sid).
	var currentID string
	if err := db.QueryRow(
		"SELECT id FROM refresh_tokens WHERE token_hash = ?",
		auth.HashRefreshToken(login1["refresh_token"].(string)),
	).Scan(&currentID); err != nil {
		t.Fatalf("lookup current session: %v", err)
	}

	// Revoke the session that made this very request — allowed for the owner.
	w := serveAuthenticated(router, http.MethodDelete, "/auth/sessions/"+currentID, login1["access_token"].(string), "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete current session: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The other session is unaffected.
	if w := refreshHelper(h, login2["refresh_token"].(string)); w.Code != http.StatusOK {
		t.Errorf("other session refresh: expected 200, got %d", w.Code)
	}
}

func TestSessions_IncludesDeviceInfo(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)
	h := newAuthHandler(db)

	// Register + login with a real User-Agent so the session records device info.
	email := "device" + uniqueSuffix() + "@example.com"
	password := "correct horse battery staple"
	if w := registerRequest(t, h, map[string]string{
		"name":     "Device Tester",
		"email":    email,
		"password": password,
	}); w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", w.Code)
	}
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Test; Chrome/120.0)")
	w := httptest.NewRecorder()
	h.Login(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", w.Code)
	}
	var loginResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&loginResp)

	w = serveAuthenticated(router, http.MethodGet, "/auth/sessions", loginResp["access_token"].(string), "")
	if w.Code != http.StatusOK {
		t.Fatalf("sessions: expected 200, got %d", w.Code)
	}
	var sessions []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions count = %d, want 1", len(sessions))
	}
	if sessions[0]["user_agent"] != "Mozilla/5.0 (Test; Chrome/120.0)" {
		t.Errorf("user_agent = %v, want the recorded User-Agent", sessions[0]["user_agent"])
	}
	if sessions[0]["ip_address"] == "" {
		t.Error("ip_address should be present")
	}
}

func TestDeleteSession_CrossUserRejected(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)
	h := newAuthHandler(db)

	aliceLogin := logoutLoginHelper(t, h)
	var aliceSessionID string
	if err := db.QueryRow(
		"SELECT id FROM refresh_tokens WHERE token_hash = ?",
		auth.HashRefreshToken(aliceLogin["refresh_token"].(string)),
	).Scan(&aliceSessionID); err != nil {
		t.Fatalf("lookup alice session: %v", err)
	}

	bobLogin := logoutLoginHelper(t, h)

	w := serveAuthenticated(router, http.MethodDelete, "/auth/sessions/"+aliceSessionID, bobLogin["access_token"].(string), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-user delete: expected 404, got %d", w.Code)
	}

	// Alice's session still works.
	if w := refreshHelper(h, aliceLogin["refresh_token"].(string)); w.Code != http.StatusOK {
		t.Errorf("alice refresh after cross-user delete attempt: expected 200, got %d", w.Code)
	}
}

func TestDeleteSession_NotFound(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := sessionsRouter(db)

	loginResp := logoutLoginHelper(t, newAuthHandler(db))

	w := serveAuthenticated(router, http.MethodDelete, "/auth/sessions/no-such-session", loginResp["access_token"].(string), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("delete unknown session: expected 404, got %d", w.Code)
	}
}
