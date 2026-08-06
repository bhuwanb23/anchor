package ratelimit

import "time"

// Default limits from the Layer 5A Step 8A / Layer 6 Step 2E plans: login is
// capped both per IP (10/15min) and per email (5/15min); register, forgot-
// password, and reset-password are also rate limited to blunt brute force,
// account spam, and email-bomb abuse. No account is ever locked — a legit
// user is only asked to wait a few minutes.
const (
	LoginIPLimit     = 10
	LoginEmailLimit  = 5
	RegisterIPLimit  = 5
	ForgotIPLimit    = 5
	ForgotEmailLimit = 3
	ResetIPLimit     = 10
	Window           = 15 * time.Minute
	RegisterWindow   = time.Hour
)

// Limiter bundles the per-endpoint stores checked by the auth handlers.
type Limiter struct {
	loginIP     *Store
	loginEmail  *Store
	forgotIP    *Store
	forgotEmail *Store
	resetIP     *Store
	registerIP  *Store
}

// New returns a Limiter with the default limits.
func New() *Limiter {
	return NewWithLimits(LoginIPLimit, LoginEmailLimit, ForgotIPLimit, ForgotEmailLimit, ResetIPLimit, Window, RegisterIPLimit)
}

// NewWithLimits builds a Limiter with explicit limits (tests use small
// numbers so they do not have to wait out the real window). The register
// limit is an optional variadic so existing call sites that predate Layer 6
// Step 2E keep compiling; when omitted it defaults to RegisterIPLimit with a
// RegisterWindow. (Register always uses the hourly window — the other stores
// share the passed window.)
func NewWithLimits(loginIP, loginEmail, forgotIP, forgotEmail, resetIP int, window time.Duration, register ...int) *Limiter {
	registerLimit := RegisterIPLimit
	if len(register) > 0 {
		registerLimit = register[0]
	}
	return &Limiter{
		loginIP:     NewStore(loginIP, window),
		loginEmail:  NewStore(loginEmail, window),
		forgotIP:    NewStore(forgotIP, window),
		forgotEmail: NewStore(forgotEmail, window),
		resetIP:     NewStore(resetIP, window),
		registerIP:  NewStore(registerLimit, RegisterWindow),
	}
}

// RegisterAllowed enforces the per-IP registration limit (Layer 6 Step 2E:
// 5 registrations per IP per hour, default). Registration spam is per-IP
// only — there is no per-email dimension because each registration is a new
// account.
func (l *Limiter) RegisterAllowed(ip string) (ok bool, retryAfter time.Duration) {
	return l.registerIP.Allow("ip:" + ip)
}

// LoginAllowed enforces the per-IP and per-email login limits. The email must
// already be normalized so equivalent case variants share one counter.
func (l *Limiter) LoginAllowed(ip, email string) (ok bool, retryAfter time.Duration) {
	if ok, retry := l.loginIP.Allow("ip:" + ip); !ok {
		return false, retry
	}
	return l.loginEmail.Allow("email:" + email)
}

// ForgotAllowed enforces the per-IP and per-email forgot-password limits.
func (l *Limiter) ForgotAllowed(ip, email string) (ok bool, retryAfter time.Duration) {
	if ok, retry := l.forgotIP.Allow("ip:" + ip); !ok {
		return false, retry
	}
	return l.forgotEmail.Allow("email:" + email)
}

// ResetAllowed enforces the per-IP reset-password limit. Reset tokens are
// single-use and unforgeable, so per-IP throttling alone is sufficient.
func (l *Limiter) ResetAllowed(ip string) (ok bool, retryAfter time.Duration) {
	return l.resetIP.Allow("ip:" + ip)
}
