package dns

import (
	"context"
	"fmt"
	"net"
	"time"
)

// VerifyDomainResolves checks if domain resolves to the expected IP address.
// Returns the first resolved IP, whether it matches, and any error.
func VerifyDomainResolves(ctx context.Context, domain, expectedIP string) (resolvedIP string, ok bool, err error) {
 resolver := &net.Resolver{
  PreferGo: true,
 }

 ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
 defer cancel()

 addrs, err := resolver.LookupHost(ctx, domain)
 if err != nil {
  return "", false, fmt.Errorf("DNS lookup for %s: %w", domain, err)
 }

 if len(addrs) == 0 {
  return "", false, fmt.Errorf("no DNS records found for %s", domain)
 }

 for _, addr := range addrs {
  if addr == expectedIP {
   return addr, true, nil
  }
 }

 return addrs[0], false, nil
}
