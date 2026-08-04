package auth

import (
	"strings"
	"testing"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr error
	}{
		{"valid basic", "user@example.com", nil},
		{"valid with plus", "user+tag@example.co", nil},
		{"valid uppercase", "User@Example.com", nil},
		{"empty", "", ErrEmailRequired},
		{"only spaces", "   ", ErrEmailRequired},
		{"no at sign", "userexample.com", ErrEmailInvalid},
		{"two at signs", "a@b@c.com", ErrEmailInvalid},
		{"empty local", "@example.com", ErrEmailInvalid},
		{"empty domain", "user@", ErrEmailInvalid},
		{"no dot in domain", "user@example", ErrEmailInvalid},
		{"no tld", "user@example.c", ErrEmailInvalid},
		{"trailing dot", "user@example.com.", ErrEmailInvalid},
		{"contains space", "user name@example.com", ErrEmailInvalid},
		{"over 254 chars", strings.Repeat("a", 246) + "@example.com", ErrEmailTooLong},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmail(tt.email)
			if err != tt.wantErr {
				t.Errorf("ValidateEmail(%q) = %v, want %v", tt.email, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	got := NormalizeEmail("  User.Example@EXAMPLE.com  ")
	if got != "user.example@example.com" {
		t.Errorf("NormalizeEmail = %q, want %q", got, "user.example@example.com")
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{"valid long", "correct horse battery staple", nil},
		{"valid special chars", "p@ssw0rd!-long-enough", nil},
		{"exactly 8 chars", "1234567a", nil},
		{"empty", "", ErrPasswordRequired},
		{"too short", "short1", ErrPasswordTooShort},
		{"7 chars", "1234567", ErrPasswordTooShort},
		{"common password", "password", ErrPasswordCommon},
		{"common password number", "12345678", ErrPasswordCommon},
		{"common uppercase", "Password123", ErrPasswordCommon},
		{"qwerty123", "qwerty123", ErrPasswordCommon},
		{"no max length", strings.Repeat("x", 200), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if err != tt.wantErr {
				t.Errorf("ValidatePassword(%q) = %v, want %v", tt.password, err, tt.wantErr)
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr error
	}{
		{"Alice Smith", nil},
		{"J", ErrNameTooShort},
		{"", ErrNameRequired},
		{"   ", ErrNameRequired},
		{strings.Repeat("x", 101), ErrNameTooLong},
		{strings.Repeat("x", 100), nil},
		{"José María", nil},
		{"山田 太郎", nil},
		{"  Trimmed Name  ", nil}, // whitespace is trimmed, name valid
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.name)
			if err != tt.wantErr {
				t.Errorf("ValidateName(%q) = %v, want %v", tt.name, err, tt.wantErr)
			}
		})
	}
}
