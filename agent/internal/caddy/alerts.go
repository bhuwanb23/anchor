package caddy

import (
	"fmt"
	"time"
)

// ErrorAlert represents an actionable error alert to send to the browser.
type ErrorAlert struct {
	Level   string `json:"level"`   // "warning" | "critical"
	Type    string `json:"type"`    // "502_errors" | "container_down" | "rate_limit" | "cert_failed" | "port_mismatch"
	Domain  string `json:"domain"`
	Message string `json:"message"` // plain-English explanation with next steps
}

// Alert502Errors creates an alert when a domain is returning 502 errors.
func Alert502Errors(domain string, failures, total int) ErrorAlert {
	return ErrorAlert{
		Level:  "warning",
		Type:   "502_errors",
		Domain: domain,
		Message: fmt.Sprintf(
			"Your app %s is returning errors. %d out of the last %d requests failed with 502 Bad Gateway. "+
				"This usually means the app container is stopped, starting up, or crashing. "+
				"Check your deployment logs for crash information.",
			domain, failures, total,
		),
	}
}

// AlertContainerDown creates an alert when the upstream container is not running.
func AlertContainerDown(domain, projectName string) ErrorAlert {
	return ErrorAlert{
		Level:  "critical",
		Type:   "container_down",
		Domain: domain,
		Message: fmt.Sprintf(
			"Your app %s is down. The container for project %s is not running. "+
				"The app will not be accessible until the container is restarted. "+
				"Check your deployment logs or restart the project from the dashboard.",
			domain, projectName,
		),
	}
}

// AlertContainerUpButFailing creates an alert when the container is running but returning errors.
func AlertContainerUpButFailing(domain, projectName string) ErrorAlert {
	return ErrorAlert{
		Level:  "warning",
		Type:   "container_up_failing",
		Domain: domain,
		Message: fmt.Sprintf(
			"Your app %s is running but returning errors. The container for project %s is up, "+
				"but it is rejecting connections or returning invalid responses. "+
				"Check your application logs for runtime errors.",
			domain, projectName,
		),
	}
}

// AlertRateLimit creates an alert when Let's Encrypt rate limit is hit.
func AlertRateLimit(domain string, resetDate time.Time) ErrorAlert {
	daysUntilReset := int(time.Until(resetDate).Hours() / 24)
	if daysUntilReset < 1 {
		daysUntilReset = 1
	}
	return ErrorAlert{
		Level:  "warning",
		Type:   "rate_limit",
		Domain: domain,
		Message: fmt.Sprintf(
			"HTTPS certificate for %s could not be issued due to a Let's Encrypt rate limit. "+
				"This is caused by too many certificate requests for this domain. "+
				"The limit resets in approximately %d days. "+
				"Your app is accessible at its platform subdomain in the meantime.",
			domain, daysUntilReset,
		),
	}
}

// AlertCertFailed creates an alert when certificate issuance fails for a non-rate-limit reason.
func AlertCertFailed(domain, reason string) ErrorAlert {
	return ErrorAlert{
		Level:  "critical",
		Type:   "cert_failed",
		Domain: domain,
		Message: fmt.Sprintf(
			"HTTPS certificate for %s could not be issued. Reason: %s. "+
				"Common causes: DNS not pointing to this server, port 80 is blocked, or domain verification failed. "+
				"Your app is accessible at its platform subdomain in the meantime.",
			domain, reason,
		),
	}
}

// AlertPortMismatch creates an alert when a Caddy route points to the wrong port.
func AlertPortMismatch(domain string, oldPort, newPort int) ErrorAlert {
	return ErrorAlert{
		Level:  "warning",
		Type:   "port_mismatch",
		Domain: domain,
		Message: fmt.Sprintf(
			"Your app %s was being proxied to port %d but the container is now on port %d. "+
				"The route has been automatically updated. "+
				"If you continue to see errors, restart the project from the dashboard.",
			domain, oldPort, newPort,
		),
	}
}
