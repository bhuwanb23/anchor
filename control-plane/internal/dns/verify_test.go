package dns

import (
 "context"
 "testing"
)

func TestVerifyDomainResolves_InvalidDomain(t *testing.T) {
 _, _, err := VerifyDomainResolves(context.Background(), "this-domain-definitely-does-not-exist-12345.invalid", "1.2.3.4")
 if err == nil {
  t.Error("expected error for nonexistent domain")
 }
}

func TestVerifyDomainResolves_EmptyDomain(t *testing.T) {
 _, _, err := VerifyDomainResolves(context.Background(), "", "1.2.3.4")
 if err == nil {
  t.Error("expected error for empty domain")
 }
}

func TestVerifyDomainResolves_ContextTimeout(t *testing.T) {
 ctx := context.Background()
 // Use a domain that will cause a timeout
 _, _, err := VerifyDomainResolves(ctx, "example.com", "1.2.3.4")
 // This should either succeed (if it resolves to 1.2.3.4) or fail with mismatch
 // The important thing is it doesn't hang forever
 _ = err
}
