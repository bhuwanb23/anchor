package caddy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDomainAuthorizer_SetDomains(t *testing.T) {
	da := NewDomainAuthorizer()

	da.SetDomains([]string{"a.example.com", "b.example.com"})

	if !da.IsAuthorized("a.example.com") {
		t.Error("a.example.com should be authorized")
	}
	if !da.IsAuthorized("b.example.com") {
		t.Error("b.example.com should be authorized")
	}
	if da.IsAuthorized("c.example.com") {
		t.Error("c.example.com should not be authorized")
	}
}

func TestDomainAuthorizer_AddDomain(t *testing.T) {
	da := NewDomainAuthorizer()

	da.AddDomain("new.example.com")

	if !da.IsAuthorized("new.example.com") {
		t.Error("new.example.com should be authorized after AddDomain")
	}
}

func TestDomainAuthorizer_RemoveDomain(t *testing.T) {
	da := NewDomainAuthorizer()

	da.AddDomain("remove-me.example.com")
	if !da.IsAuthorized("remove-me.example.com") {
		t.Fatal("domain should be authorized before removal")
	}

	da.RemoveDomain("remove-me.example.com")
	if da.IsAuthorized("remove-me.example.com") {
		t.Error("domain should not be authorized after removal")
	}
}

func TestDomainAuthorizer_SetDomains_ReplacesAll(t *testing.T) {
	da := NewDomainAuthorizer()

	da.AddDomain("old.example.com")
	da.SetDomains([]string{"new.example.com"})

	if da.IsAuthorized("old.example.com") {
		t.Error("old.example.com should not be authorized after SetDomains")
	}
	if !da.IsAuthorized("new.example.com") {
		t.Error("new.example.com should be authorized after SetDomains")
	}
}

func TestDomainAuthorizer_HandleAsk_Authorized(t *testing.T) {
	da := NewDomainAuthorizer()
	da.AddDomain("authorized.example.com")

	req := httptest.NewRequest(http.MethodGet, "/__yourplatform_ask?domain=authorized.example.com", nil)
	w := httptest.NewRecorder()

	da.HandleAsk(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for authorized domain, got %d", w.Code)
	}
}

func TestDomainAuthorizer_HandleAsk_Unauthorized(t *testing.T) {
	da := NewDomainAuthorizer()

	req := httptest.NewRequest(http.MethodGet, "/__yourplatform_ask?domain=unauthorized.example.com", nil)
	w := httptest.NewRecorder()

	da.HandleAsk(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for unauthorized domain, got %d", w.Code)
	}
}

func TestDomainAuthorizer_HandleAsk_MissingDomain(t *testing.T) {
	da := NewDomainAuthorizer()

	req := httptest.NewRequest(http.MethodGet, "/__yourplatform_ask", nil)
	w := httptest.NewRecorder()

	da.HandleAsk(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing domain, got %d", w.Code)
	}
}

func TestDomainAuthorizer_HandleAsk_EmptyDomain(t *testing.T) {
	da := NewDomainAuthorizer()

	req := httptest.NewRequest(http.MethodGet, "/__yourplatform_ask?domain=", nil)
	w := httptest.NewRecorder()

	da.HandleAsk(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty domain, got %d", w.Code)
	}
}

func TestDomainAuthorizer_Concurrent(t *testing.T) {
	da := NewDomainAuthorizer()

	// Run concurrent reads and writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				da.AddDomain("test.example.com")
				da.IsAuthorized("test.example.com")
				da.RemoveDomain("test.example.com")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
