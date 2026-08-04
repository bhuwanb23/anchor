package auth

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Validation errors — each maps to a specific 400 response field error in
// the registration handler.
var (
	ErrEmailRequired    = errors.New("email is required")
	ErrEmailInvalid     = errors.New("invalid email address")
	ErrEmailTooLong     = errors.New("email must be at most 254 characters")
	ErrPasswordRequired = errors.New("password is required")
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")
	ErrPasswordCommon   = errors.New("this password is too common, please choose a stronger one")
	ErrNameRequired     = errors.New("name is required")
	ErrNameTooShort     = errors.New("name must be at least 2 characters")
	ErrNameTooLong      = errors.New("name must be at most 100 characters")
)

// maxEmailLen is the RFC 5321 mailbox limit.
const maxEmailLen = 254

// minPasswordLen follows NIST 800-63B guidance: length over complexity.
const minPasswordLen = 8

// NormalizeEmail lowercases and trims an email before storage and lookup so
// accounts are treated case-insensitively.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail performs deliberately simple checks (per the Layer 5A plan):
// non-empty, within RFC 5321 length, contains exactly one @ with a non-empty
// local part, and a domain with a TLD. It is not a full RFC validator — the
// future email-verification step catches edge cases.
func ValidateEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return ErrEmailRequired
	}
	if len(email) > maxEmailLen {
		return ErrEmailTooLong
	}
	if strings.ContainsAny(email, " \t\r\n") {
		return ErrEmailInvalid
	}

	// Exactly one @: a non-empty local part and a non-empty domain.
	if strings.Count(email, "@") != 1 {
		return ErrEmailInvalid
	}

	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return ErrEmailInvalid
	}

	domain := email[at+1:]
	if !strings.Contains(domain, ".") {
		return ErrEmailInvalid
	}

	// The TLD must be at least 2 characters (e.g. .com, .io, .app).
	dot := strings.LastIndex(domain, ".")
	if dot == len(domain)-1 || len(domain)-dot-1 < 2 {
		return ErrEmailInvalid
	}

	return nil
}

// ValidatePassword enforces a minimum length and rejects a list of common
// passwords. It deliberately imposes no maximum length and no complexity
// rules (they produce predictable patterns and break password managers).
func ValidatePassword(password string) error {
	if password == "" {
		return ErrPasswordRequired
	}
	if len(password) < minPasswordLen {
		return ErrPasswordTooShort
	}
	if isCommonPassword(password) {
		return ErrPasswordCommon
	}
	return nil
}

// ValidateName trims surrounding whitespace and enforces 2–100 characters.
// Any Unicode is allowed (international names).
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNameRequired
	}
	n := utf8.RuneCountInString(name)
	if n < 2 {
		return ErrNameTooShort
	}
	if n > 100 {
		return ErrNameTooLong
	}
	return nil
}

// commonPasswords is the denial list checked on registration. Comparison is
// case-insensitive; the list uses lowercase entries.
var commonPasswords = map[string]struct{}{
	"password":    {},
	"password1":   {},
	"password123": {},
	"12345678":    {},
	"123456789":   {},
	"1234567890":  {},
	"12345678910": {},
	"qwerty":      {},
	"qwerty123":   {},
	"qwertyuiop":  {},
	"abc123":      {},
	"letmein":     {},
	"admin":       {},
	"welcome":     {},
	"monkey":      {},
	"dragon":      {},
	"iloveyou":    {},
	"trustno1":    {},
	"football":    {},
	"baseball":    {},
	"sunshine":    {},
	"princess":    {},
	"superman":    {},
	"batman":      {},
	"11111111":    {},
	"00000000":    {},
	"changeme":    {},
	"hello123":    {},
}

func isCommonPassword(password string) bool {
	_, ok := commonPasswords[strings.ToLower(password)]
	return ok
}
