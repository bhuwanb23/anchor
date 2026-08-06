package handlers_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yourname/yourplatform/control-plane/internal/api/handlers"
)

// decodeBody builds a request with the given JSON body and runs DecodeJSON
// through a real ResponseWriter (MaxBytesReader requires one).
func decodeBody(t *testing.T, body string, dst interface{}) error {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	return handlers.DecodeJSON(w, req, dst)
}

// ---------------------------------------------------------------------------
// DecodeJSON
// ---------------------------------------------------------------------------

func TestDecodeJSON_ValidBody(t *testing.T) {
	var dst struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := decodeBody(t, `{"name":"alice","count":3}`, &dst); err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if dst.Name != "alice" || dst.Count != 3 {
		t.Errorf("decoded = %+v, want {alice 3}", dst)
	}
}

func TestDecodeJSON_EmptyBody(t *testing.T) {
	var dst map[string]string
	if err := decodeBody(t, ``, &dst); err == nil {
		t.Fatal("expected error for empty body")
	} else if !strings.Contains(err.Error(), "Request body is empty") {
		t.Errorf("error = %q, want empty-body message", err.Error())
	}
}

func TestDecodeJSON_InvalidJSON(t *testing.T) {
	var dst map[string]string
	err := decodeBody(t, `{"name": }`, &dst)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "Invalid JSON in request body") {
		t.Errorf("error = %q, want invalid-JSON message", err.Error())
	}
}

func TestDecodeJSON_IncompleteJSON(t *testing.T) {
	var dst map[string]string
	err := decodeBody(t, `{"name": "alice"`, &dst)
	if err == nil {
		t.Fatal("expected error for truncated JSON")
	}
	if !strings.Contains(err.Error(), "Request body is incomplete") {
		t.Errorf("error = %q, want incomplete-body message", err.Error())
	}
}

func TestDecodeJSON_UnknownField(t *testing.T) {
	var dst struct {
		Name string `json:"name"`
	}
	err := decodeBody(t, `{"name":"alice","bogus":1}`, &dst)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), `Unknown field "bogus"`) {
		t.Errorf("error = %q, want unknown-field message naming bogus", err.Error())
	}
}

func TestDecodeJSON_TypeMismatch(t *testing.T) {
	var dst struct {
		Count int `json:"count"`
	}
	err := decodeBody(t, `{"count":"many"}`, &dst)
	if err == nil {
		t.Fatal("expected error for type mismatch")
	}
	if !strings.Contains(err.Error(), "count") || !strings.Contains(err.Error(), "int") {
		t.Errorf("error = %q, want type error naming field and type", err.Error())
	}
}

func TestDecodeJSON_TrailingDataRejected(t *testing.T) {
	var dst map[string]string
	// Two JSON documents in one body: the first decodes fine, the trailing
	// second one must be rejected (not silently ignored).
	if err := decodeBody(t, `{"a":"1"}{"b":"2"}`, &dst); err == nil {
		t.Fatal("expected error for trailing JSON data")
	} else if !strings.Contains(err.Error(), "single JSON value") {
		t.Errorf("error = %q, want single-value message", err.Error())
	}
	// Trailing whitespace is fine.
	if err := decodeBody(t, "{\"a\":\"1\"}   \n\t", &dst); err != nil {
		t.Errorf("trailing whitespace should pass: %v", err)
	}
}

func TestDecodeJSON_OversizedBody(t *testing.T) {
	var dst map[string]string
	// 1MB + 1 byte forces MaxBytesReader to trip.
	big := strings.Repeat("a", 1<<20+1)
	err := decodeBody(t, `{"name":"`+big+`"}`, &dst)
	if err == nil {
		t.Fatal("expected error for oversized body")
	}
	if !strings.Contains(err.Error(), "at most") {
		t.Errorf("error = %q, want size-limit message", err.Error())
	}
}

// ---------------------------------------------------------------------------
// ValidationErrors
// ---------------------------------------------------------------------------

func TestValidationErrors_AddAndHasErrors(t *testing.T) {
	ve := handlers.ValidationErrors{}
	if ve.HasErrors() {
		t.Error("fresh ValidationErrors must not have errors")
	}
	ve.Add("email", "Must be a valid email address")
	if !ve.HasErrors() {
		t.Error("after Add, HasErrors must be true")
	}
	if got := ve["email"]; got != "Must be a valid email address" {
		t.Errorf("email message = %q", got)
	}
	// Later Add overwrites.
	ve.Add("email", "Email is required")
	if got := ve["email"]; got != "Email is required" {
		t.Errorf("email message after overwrite = %q", got)
	}
}

func TestValidationErrors_Response(t *testing.T) {
	ve := handlers.ValidationErrors{}
	ve.Add("email", "Must be a valid email address")
	ve.Add("password", "Must be at least 8 characters")

	status, body := ve.Response()
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
	m, ok := body.(map[string]interface{})
	if !ok {
		t.Fatalf("body type = %T, want map", body)
	}
	if m["error"] != "validation_failed" {
		t.Errorf("error = %v, want validation_failed", m["error"])
	}
	if m["message"] != "Request validation failed" {
		t.Errorf("message = %v", m["message"])
	}
	fields, ok := m["fields"].(map[string]string)
	if !ok {
		t.Fatalf("fields type = %T, want map[string]string", m["fields"])
	}
	if len(fields) != 2 {
		t.Errorf("fields count = %d, want 2 (ALL errors reported)", len(fields))
	}
	if fields["email"] != "Must be a valid email address" {
		t.Errorf("fields.email = %q", fields["email"])
	}
	if fields["password"] != "Must be at least 8 characters" {
		t.Errorf("fields.password = %q", fields["password"])
	}
}

// ---------------------------------------------------------------------------
// Register validation (Step 3B)
// ---------------------------------------------------------------------------

func TestValidateRegister_ValidInput(t *testing.T) {
	ve := handlers.ValidateRegisterRequest("alice@example.com", "correct horse battery staple", "Alice Smith")
	if ve.HasErrors() {
		t.Errorf("unexpected errors: %v", ve)
	}
}

func TestValidateRegister_AllFieldErrorsAtOnce(t *testing.T) {
	ve := handlers.ValidateRegisterRequest("", "short", "J")
	if !ve.HasErrors() {
		t.Fatal("expected errors")
	}
	// ALL invalid fields reported together, never just the first.
	for _, field := range []string{"email", "password", "name"} {
		if _, ok := ve[field]; !ok {
			t.Errorf("expected error for field %q, got %v", field, ve)
		}
	}
}

func TestValidateRegister_EmailRules(t *testing.T) {
	cases := []struct {
		email string
		msg   string
	}{
		{"", "Email is required"},
		{"not-an-email", "Must be a valid email address"},
		{"user@nodot", "Must be a valid email address"},
		{strings.Repeat("a", 250) + "@example.com", "Must be at most 254 characters"},
	}
	for _, tc := range cases {
		ve := handlers.ValidateRegisterRequest(tc.email, "correct horse battery staple", "Alice Smith")
		got, ok := ve["email"]
		if !ok {
			t.Errorf("email %q: expected field error, got none", tc.email)
			continue
		}
		if got != tc.msg {
			t.Errorf("email %q: message = %q, want %q", tc.email, got, tc.msg)
		}
	}
}

func TestValidateRegister_PasswordRules(t *testing.T) {
	cases := []struct {
		password string
		msg      string
	}{
		{"", "Password is required"},
		{"short", "Must be at least 8 characters"},
		{"password", "This password is too common, please choose a stronger one"},
	}
	for _, tc := range cases {
		ve := handlers.ValidateRegisterRequest("alice@example.com", tc.password, "Alice Smith")
		got, ok := ve["password"]
		if !ok {
			t.Errorf("password %q: expected field error, got none", tc.password)
			continue
		}
		if got != tc.msg {
			t.Errorf("password %q: message = %q, want %q", tc.password, got, tc.msg)
		}
	}
}

func TestValidateRegister_NameRules(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"", "Name is required"},
		{"J", "Must be at least 2 characters"},
		{strings.Repeat("x", 101), "Must be at most 100 characters"},
	}
	for _, tc := range cases {
		ve := handlers.ValidateRegisterRequest("alice@example.com", "correct horse battery staple", tc.name)
		got, ok := ve["name"]
		if !ok {
			t.Errorf("name %q: expected field error, got none", tc.name)
			continue
		}
		if got != tc.msg {
			t.Errorf("name %q: message = %q, want %q", tc.name, got, tc.msg)
		}
	}
}

// ---------------------------------------------------------------------------
// Deploy validation (Step 3B)
// ---------------------------------------------------------------------------

func intPtr(v int) *int { return &v }

func TestValidateDeploy_ValidInput(t *testing.T) {
	req := &handlers.DeployRequest{
		Image:       "ghcr.io/acme/app:v1.2.3",
		Port:        8080,
		ProjectName: "myapp",
	}
	ve := handlers.ValidateDeployRequest(req)
	if ve.HasErrors() {
		t.Errorf("unexpected errors: %v", ve)
	}
	// Defaults applied.
	if req.MemoryLimitMB == nil || *req.MemoryLimitMB != 512 {
		t.Errorf("memory default = %v, want 512", req.MemoryLimitMB)
	}
	if req.CPUQuotaPercent == nil || *req.CPUQuotaPercent != 50 {
		t.Errorf("cpu default = %v, want 50", req.CPUQuotaPercent)
	}
}

func TestValidateDeploy_ImageRules(t *testing.T) {
	cases := []struct {
		image string
		msg   string
	}{
		{"", "Image is required"},
		{"UPPER/IMAGE", "Must be a valid image reference"},
		{strings.Repeat("a", 501), "Must be at most 500 characters"},
		{"has space", "Must be a valid image reference"},
	}
	for _, tc := range cases {
		req := &handlers.DeployRequest{Image: tc.image, Port: 8080, ProjectName: "myapp"}
		ve := handlers.ValidateDeployRequest(req)
		got, ok := ve["image"]
		if !ok {
			t.Errorf("image %q: expected field error, got none", tc.image)
			continue
		}
		if !strings.Contains(got, tc.msg) {
			t.Errorf("image %q: message = %q, want contains %q", tc.image, got, tc.msg)
		}
	}
}

func TestValidateDeploy_PortRules(t *testing.T) {
	// Missing port.
	req := &handlers.DeployRequest{Image: "nginx", Port: 0, ProjectName: "myapp"}
	ve := handlers.ValidateDeployRequest(req)
	if _, ok := ve["port"]; !ok {
		t.Error("expected port required error")
	}

	// Out of range.
	req = &handlers.DeployRequest{Image: "nginx", Port: 70000, ProjectName: "myapp"}
	ve = handlers.ValidateDeployRequest(req)
	if _, ok := ve["port"]; !ok {
		t.Error("expected port range error")
	}

	// Privileged ports are allowed (warning, not block).
	req = &handlers.DeployRequest{Image: "nginx", Port: 80, ProjectName: "myapp"}
	ve = handlers.ValidateDeployRequest(req)
	if _, ok := ve["port"]; ok {
		t.Error("privileged port must not be blocked")
	}
	if !handlers.IsPrivilegedPort(80) {
		t.Error("IsPrivilegedPort(80) must be true")
	}
	if handlers.IsPrivilegedPort(8080) {
		t.Error("IsPrivilegedPort(8080) must be false")
	}
}

func TestValidateDeploy_ResourceRules(t *testing.T) {
	// memory out of range.
	req := &handlers.DeployRequest{Image: "nginx", Port: 8080, ProjectName: "myapp", MemoryLimitMB: intPtr(32)}
	ve := handlers.ValidateDeployRequest(req)
	if _, ok := ve["memory_limit_mb"]; !ok {
		t.Error("expected memory_limit_mb range error")
	}

	// memory at boundaries is fine.
	req = &handlers.DeployRequest{Image: "nginx", Port: 8080, ProjectName: "myapp", MemoryLimitMB: intPtr(64)}
	if ve := handlers.ValidateDeployRequest(req); ve.HasErrors() {
		t.Errorf("memory 64 should pass: %v", ve)
	}

	// cpu out of range.
	req = &handlers.DeployRequest{Image: "nginx", Port: 8080, ProjectName: "myapp", CPUQuotaPercent: intPtr(5)}
	ve = handlers.ValidateDeployRequest(req)
	if _, ok := ve["cpu_quota_percent"]; !ok {
		t.Error("expected cpu_quota_percent range error")
	}

	// explicit cpu is kept (no default overwrite).
	req = &handlers.DeployRequest{Image: "nginx", Port: 8080, ProjectName: "myapp", CPUQuotaPercent: intPtr(80)}
	if ve := handlers.ValidateDeployRequest(req); ve.HasErrors() {
		t.Errorf("cpu 80 should pass: %v", ve)
	}
	if req.CPUQuotaPercent == nil || *req.CPUQuotaPercent != 80 {
		t.Errorf("explicit cpu was overwritten: %v", req.CPUQuotaPercent)
	}
}

func TestValidateDeploy_ProjectNameRules(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"", "Project name is required"},
		{"1start", "Must start with a letter"},
		{"UPPER", "Must start with a letter"},
		{"has space", "Must start with a letter"},
		{strings.Repeat("a", 64), "Must start with a letter"}, // > 63 chars
	}
	for _, tc := range cases {
		req := &handlers.DeployRequest{Image: "nginx", Port: 8080, ProjectName: tc.name}
		ve := handlers.ValidateDeployRequest(req)
		got, ok := ve["project_name"]
		if !ok {
			t.Errorf("project_name %q: expected field error, got none", tc.name)
			continue
		}
		if !strings.Contains(got, tc.msg) {
			t.Errorf("project_name %q: message = %q, want contains %q", tc.name, got, tc.msg)
		}
	}
}

func TestValidateDeploy_ValidProjectNames(t *testing.T) {
	for _, name := range []string{"a", "myapp", "my-app-2", "a" + strings.Repeat("-x", 31)} {
		req := &handlers.DeployRequest{Image: "nginx", Port: 8080, ProjectName: name}
		if ve := handlers.ValidateDeployRequest(req); ve.HasErrors() {
			t.Errorf("project_name %q should pass: %v", name, ve)
		}
	}
}

// ---------------------------------------------------------------------------
// Domain validation (Step 3B)
// ---------------------------------------------------------------------------

func TestValidateDomain_Valid(t *testing.T) {
	for _, domain := range []string{"example.com", "app.example.com", "my-app.io", "sub.domain.co.uk"} {
		ve := handlers.ValidateDomainInput(domain, "yourplatform.app")
		if ve.HasErrors() {
			t.Errorf("domain %q should pass: %v", domain, ve)
		}
	}
}

func TestValidateDomain_Invalid(t *testing.T) {
	cases := []struct {
		domain string
		msg    string
	}{
		{"", "Domain is required"},
		{"-bad.com", "Must be a valid hostname"},
		{"bad-.com", "Must be a valid hostname"},
		{"bad..com", "Must be a valid hostname"},
		{"has space.com", "Must be a valid hostname"},
		{"under_score.com", "Must be a valid hostname"},
		{strings.Repeat("a", 254) + ".com", "Must be at most 253 characters"},
	}
	for _, tc := range cases {
		ve := handlers.ValidateDomainInput(tc.domain, "yourplatform.app")
		got, ok := ve["domain"]
		if !ok {
			t.Errorf("domain %q: expected field error, got none", tc.domain)
			continue
		}
		if !strings.Contains(got, tc.msg) {
			t.Errorf("domain %q: message = %q, want contains %q", tc.domain, got, tc.msg)
		}
	}
}

func TestValidateDomain_ReservedPlatformSubdomain(t *testing.T) {
	// The platform root and anything under it is reserved.
	for _, domain := range []string{"yourplatform.app", "foo.yourplatform.app", "a.b.yourplatform.app"} {
		ve := handlers.ValidateDomainInput(domain, "yourplatform.app")
		got, ok := ve["domain"]
		if !ok {
			t.Errorf("domain %q: expected reserved error, got none", domain)
			continue
		}
		if !strings.Contains(got, "reserved") {
			t.Errorf("domain %q: message = %q, want reserved", domain, got)
		}
	}
	// Case-insensitive comparison.
	ve := handlers.ValidateDomainInput("Foo.YOURPLATFORM.APP", "yourplatform.app")
	if _, ok := ve["domain"]; !ok {
		t.Error("reserved check must be case-insensitive")
	}
	// A similar-but-different domain is fine.
	ve = handlers.ValidateDomainInput("yourplatform.app.evil.com", "yourplatform.app")
	if ve.HasErrors() {
		t.Errorf("lookalike domain should pass: %v", ve)
	}
}

// ---------------------------------------------------------------------------
// Env key/value validation (Step 3B)
// ---------------------------------------------------------------------------

func TestValidateEnvKey_Valid(t *testing.T) {
	for _, key := range []string{"A", "DATABASE_URL", "NODE_ENV", "MY_VAR_2", strings.Repeat("K", 256)} {
		ve := handlers.ValidateEnvKey(key)
		if ve.HasErrors() {
			t.Errorf("key %q should pass: %v", key, ve)
		}
	}
}

func TestValidateEnvKey_Invalid(t *testing.T) {
	cases := []struct {
		key string
		msg string
	}{
		{"", "required"},
		{"lowercase", "uppercase"},
		{"1STARTS_WITH_DIGIT", "uppercase"},
		{strings.Repeat("K", 257), "256"},
		{"PORT", "reserved"},
		{"YOURPLATFORM", "reserved"},
		{"YOURPLATFORM_TOKEN", "reserved"},
	}
	for _, tc := range cases {
		ve := handlers.ValidateEnvKey(tc.key)
		got, ok := ve["key"]
		if !ok {
			t.Errorf("key %q: expected field error, got none", tc.key)
			continue
		}
		if !strings.Contains(got, tc.msg) {
			t.Errorf("key %q: message = %q, want contains %q", tc.key, got, tc.msg)
		}
	}
}

func TestValidateEnvValue(t *testing.T) {
	// Missing value → required error.
	ve := handlers.ValidateEnvValue(nil)
	if _, ok := ve["value"]; !ok {
		t.Error("expected value required error for nil")
	}

	// Empty string is valid (present but empty).
	empty := ""
	if ve := handlers.ValidateEnvValue(&empty); ve.HasErrors() {
		t.Errorf("empty value should pass: %v", ve)
	}

	// 32KB is the cap.
	ok := strings.Repeat("v", 32*1024)
	if ve := handlers.ValidateEnvValue(&ok); ve.HasErrors() {
		t.Errorf("32KB value should pass: %v", ve)
	}
	tooBig := strings.Repeat("v", 32*1024+1)
	if ve := handlers.ValidateEnvValue(&tooBig); ve.HasErrors() == false {
		t.Error("expected size error for >32KB value")
	}
}


