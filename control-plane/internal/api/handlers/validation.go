package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
)

// Layer 6 Step 3 — Request Validation.
//
// Every handler that accepts a request body follows the same sequence:
//  1. DecodeJSON (1MB cap, strict unknown-field rejection, specific errors)
//  2. Field-level validation (ValidationErrors, ALL errors at once)
//  3. Only then touch the database.
//
// Validation errors always return 400 with the Step 3C shape:
//
//	{ "error": "validation_failed", "message": "Request validation failed",
//	  "fields": { "email": "Must be a valid email address" } }
//
// Decode failures return 400 with { "error": "invalid_request", "message": ... }.

// maxBodyBytes is the request body size limit applied to every endpoint
// (Layer 6 Step 3 — "Body size limit"). 1MB is generous for any API request
// in this system; deployment configs and env vars never approach it.
const maxBodyBytes = 1 << 20 // 1MB

// DecodeJSON reads and decodes a JSON request body with:
//   - a 1MB size cap (MaxBytesReader, so oversized bodies abort the request)
//   - strict unknown-field rejection (DisallowUnknownFields) — a client that
//     sends a misspelled or stale field learns about it instead of silently
//     ignoring it
//   - specific, user-readable errors for the common failure modes
//
// NOTE: the signature takes the ResponseWriter as well as the request because
// http.MaxBytesReader needs it to flag oversized requests (plan's helper
// sketch omits it, but it is technically required).
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}

	// Reject trailing data: a body like `{"a":1}{"b":2}` decodes the first
	// value fine and would otherwise silently ignore the rest. A second
	// Decode must hit EOF (trailing whitespace is fine). RawMessage is used
	// so any trailing JSON value is accepted as "extra data" and reported as
	// such, rather than surfaced as an unrelated decode error.
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("Request body must contain a single JSON value")
		}
		return decodeError(err)
	}
	return nil
}

// decodeError maps a JSON decode failure to a specific, user-readable message.
// The error values are intentionally never echoed verbatim into responses
// unless they are one of the mapped cases below.
func decodeError(err error) error {
	// Oversized body: MaxBytesReader reports *http.MaxBytesError.
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return fmt.Errorf("Request body must be at most %d bytes", maxErr.Limit)
	}

	// DisallowUnknownFields surfaces unknown fields as "json: unknown field".
	if strings.Contains(err.Error(), "json: unknown field") {
		field := unknownFieldName(err.Error())
		return fmt.Errorf("Unknown field %q in request body", field)
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Errorf("Field %s must be %s", typeErr.Field, typeErr.Type)
	}

	var synErr *json.SyntaxError
	if errors.As(err, &synErr) {
		return errors.New("Invalid JSON in request body")
	}

	if errors.Is(err, io.EOF) {
		return errors.New("Request body is empty")
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return errors.New("Request body is incomplete")
	}

	return err
}

// unknownFieldName extracts the field name from a "json: unknown field \"x\""
// error so the message can name the offending field.
func unknownFieldName(msg string) string {
	start := strings.Index(msg, `"`)
	if start < 0 {
		return "?"
	}
	rest := msg[start+1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "?"
	}
	return rest[:end]
}

// ValidationErrors collects per-field validation messages so a handler can
// report EVERY invalid field in one response (Layer 6 Step 3C). Messages are
// written for the end user and never include the invalid value (the value may
// be a password or other sensitive data).
type ValidationErrors map[string]string

// Add records a message for a field. Later Adds for the same field overwrite
// earlier ones (the most specific rule wins).
func (ve ValidationErrors) Add(field, message string) {
	ve[field] = message
}

// HasErrors reports whether any field failed validation.
func (ve ValidationErrors) HasErrors() bool {
	return len(ve) > 0
}

// Response returns the HTTP status (always 400) and the Step 3C error body.
func (ve ValidationErrors) Response() (int, interface{}) {
	return http.StatusBadRequest, map[string]interface{}{
		"error":   "validation_failed",
		"message": "Request validation failed",
		"fields":  map[string]string(ve),
	}
}

// writeValidationError writes a validation_failed response (400) with the
// request_id attached, matching the shared error shape.
func writeValidationError(w http.ResponseWriter, r *http.Request, ve ValidationErrors) {
	status, body := ve.Response()
	m := body.(map[string]interface{})
	if rid := chimw.GetReqID(r.Context()); rid != "" {
		m["request_id"] = rid
	}
	writeJSON(w, status, m)
}

// ---------------------------------------------------------------------------
// Register (Step 3B)
// ---------------------------------------------------------------------------

// ValidateRegisterRequest validates the register payload against the Layer 6
// Step 3B rules and returns ALL field errors at once. The underlying rules
// live in internal/auth (email/password/name); this translates them into
// user-friendly field messages.
func ValidateRegisterRequest(email, password, name string) ValidationErrors {
	ve := ValidationErrors{}

	if err := auth.ValidateEmail(email); err != nil {
		switch {
		case errors.Is(err, auth.ErrEmailRequired):
			ve.Add("email", "Email is required")
		case errors.Is(err, auth.ErrEmailTooLong):
			ve.Add("email", "Must be at most 254 characters")
		default:
			ve.Add("email", "Must be a valid email address")
		}
	}

	if err := auth.ValidatePassword(password); err != nil {
		switch {
		case errors.Is(err, auth.ErrPasswordRequired):
			ve.Add("password", "Password is required")
		case errors.Is(err, auth.ErrPasswordTooShort):
			ve.Add("password", "Must be at least 8 characters")
		default: // ErrPasswordCommon
			ve.Add("password", "This password is too common, please choose a stronger one")
		}
	}

	if err := auth.ValidateName(name); err != nil {
		switch {
		case errors.Is(err, auth.ErrNameRequired):
			ve.Add("name", "Name is required")
		case errors.Is(err, auth.ErrNameTooShort):
			ve.Add("name", "Must be at least 2 characters")
		default: // ErrNameTooLong
			ve.Add("name", "Must be at most 100 characters")
		}
	}

	return ve
}

// ---------------------------------------------------------------------------
// Deploy (Step 3B) — POST /servers/{serverID}/apps/{appID}/deploy
// ---------------------------------------------------------------------------

// DeployRequest is the body of the app deploy endpoint (Layer 6 Step 5 wires
// the handler; the validator is defined and tested here in Step 3).
type DeployRequest struct {
	Image           string `json:"image"`
	Port            int    `json:"port"`
	MemoryLimitMB   *int   `json:"memory_limit_mb,omitempty"`
	CPUQuotaPercent *int   `json:"cpu_quota_percent,omitempty"`
	ProjectName     string `json:"project_name"`
}

// Defaults applied when optional deploy fields are absent (Step 3B).
const (
	defaultMemoryLimitMB   = 512
	defaultCPUQuotaPercent = 50
	minMemoryLimitMB       = 64
	maxMemoryLimitMB       = 8192
	minCPUQuotaPercent     = 10
	maxCPUQuotaPercent     = 100
)

// imageRefPattern is the allowed character set for a Docker image reference.
// Uppercase letters are excluded per the plan: [a-z0-9._/-:].
var imageRefPattern = regexp.MustCompile(`^[a-z0-9._/:-]+$`)

// projectNamePattern matches [a-z0-9-] starting with a letter, max 63 chars.
var projectNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// ValidateDeployRequest applies the Step 3B deploy rules and fills in the
// documented defaults for absent optional fields. Privileged ports (<1024) are
// ALLOWED per the plan — they warrant a warning, not a block (the Step 5
// handler can warn via IsPrivilegedPort).
func ValidateDeployRequest(req *DeployRequest) ValidationErrors {
	ve := ValidationErrors{}

	// image — required, valid reference characters, max 500.
	if req.Image == "" {
		ve.Add("image", "Image is required")
	} else {
		if utf8.RuneCountInString(req.Image) > 500 {
			ve.Add("image", "Must be at most 500 characters")
		}
		if !imageRefPattern.MatchString(req.Image) {
			ve.Add("image", "Must be a valid image reference (lowercase letters, digits, and . _ / - :)")
		}
	}

	// port — required, 1-65535.
	if req.Port == 0 {
		ve.Add("port", "Port is required")
	} else if req.Port < 1 || req.Port > 65535 {
		ve.Add("port", "Must be between 1 and 65535")
	}

	// memory_limit_mb — optional (default 512), 64-8192.
	if req.MemoryLimitMB == nil {
		v := defaultMemoryLimitMB
		req.MemoryLimitMB = &v
	} else if *req.MemoryLimitMB < minMemoryLimitMB || *req.MemoryLimitMB > maxMemoryLimitMB {
		ve.Add("memory_limit_mb", "Must be between 64 and 8192 MB")
	}

	// cpu_quota_percent — optional (default 50), 10-100.
	if req.CPUQuotaPercent == nil {
		v := defaultCPUQuotaPercent
		req.CPUQuotaPercent = &v
	} else if *req.CPUQuotaPercent < minCPUQuotaPercent || *req.CPUQuotaPercent > maxCPUQuotaPercent {
		ve.Add("cpu_quota_percent", "Must be between 10 and 100")
	}

	// project_name — required, [a-z0-9-] starting with a letter, max 63.
	if req.ProjectName == "" {
		ve.Add("project_name", "Project name is required")
	} else if !projectNamePattern.MatchString(req.ProjectName) {
		ve.Add("project_name", "Must start with a letter and contain only lowercase letters, digits, and hyphens (max 63 characters)")
	}

	return ve
}

// IsPrivilegedPort reports whether a port is below 1024. Privileged ports are
// NOT blocked by validation (Step 3B: "warning if < 1024, not a block") — the
// handler may log a warning.
func IsPrivilegedPort(port int) bool {
	return port > 0 && port < 1024
}

// ---------------------------------------------------------------------------
// Domains (Step 3B) — POST /servers/{serverID}/apps/{appID}/domains
// ---------------------------------------------------------------------------

// ValidateDomainInput validates a custom domain: required, valid hostname,
// at most 253 characters, and not a reserved platform subdomain. The
// "already used by another app" uniqueness check is a database lookup, so it
// belongs in the handler (Step 5), not here.
func ValidateDomainInput(domain, baseDomain string) ValidationErrors {
	ve := ValidationErrors{}

	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		ve.Add("domain", "Domain is required")
		return ve
	}
	if len(domain) > 253 {
		ve.Add("domain", "Must be at most 253 characters")
		return ve
	}
	if !validHostname(domain) {
		ve.Add("domain", "Must be a valid hostname (e.g. app.example.com)")
		return ve
	}

	// Reserved: the platform's own root domain and any of its subdomains.
	if baseDomain != "" {
		base := strings.ToLower(strings.TrimSuffix(baseDomain, "."))
		if domain == base || strings.HasSuffix(domain, "."+base) {
			ve.Add("domain", "This domain is reserved for the platform")
		}
	}

	return ve
}

// validHostname checks RFC 1123 hostname syntax: dot-separated labels of
// 1-63 characters from [a-z0-9-], never starting or ending with a hyphen.
func validHostname(hostname string) bool {
	if hostname == "" || len(hostname) > 253 {
		return false
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Environment variables (Step 3B) — PUT /servers/{serverID}/apps/{appID}/env/{key}
// ---------------------------------------------------------------------------

// envKeyPattern matches a valid environment variable name: [A-Z][A-Z0-9_]*.
var envKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// maxEnvKeyLen and maxEnvValueLen bound env var names and values.
const (
	maxEnvKeyLen   = 256
	maxEnvValueLen = 32 * 1024 // 32KB — some env vars are large
)

// ValidateEnvKey validates an environment variable NAME (the URL param).
// PORT and YOURPLATFORM are reserved exact names; anything starting with
// YOURPLATFORM_ is reserved for platform-injected variables.
func ValidateEnvKey(key string) ValidationErrors {
	ve := ValidationErrors{}

	if key == "" {
		ve.Add("key", "Environment variable name is required")
		return ve
	}
	if len(key) > maxEnvKeyLen {
		ve.Add("key", "Must be at most 256 characters")
		return ve
	}
	if !envKeyPattern.MatchString(key) {
		ve.Add("key", "Must start with an uppercase letter and contain only uppercase letters, digits, and underscores")
		return ve
	}
	if key == "PORT" || key == "YOURPLATFORM" || strings.HasPrefix(key, "YOURPLATFORM_") {
		ve.Add("key", "This name is reserved for the platform")
	}
	return ve
}

// ValidateEnvValue validates an environment variable VALUE. A present but
// empty value is valid (Step 3B); the value is capped at 32KB.
func ValidateEnvValue(value *string) ValidationErrors {
	ve := ValidationErrors{}

	if value == nil {
		ve.Add("value", "Value is required")
		return ve
	}
	if len(*value) > maxEnvValueLen {
		ve.Add("value", "Must be at most 32KB")
	}
	return ve
}
