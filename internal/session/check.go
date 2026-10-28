package session

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

type connectionError struct {
	stage string
	err   error
}

func (e *connectionError) Error() string { return e.err.Error() }
func (e *connectionError) Unwrap() error { return e.err }

// CheckResult reports only local guidance, never upstream response text.
type CheckResult struct {
	OK      bool
	Stage   string
	Message string
}

// Check reads one workflow page through the same session used by the TUI.
// The deadline covers forwarding, TLS and reading the response.
func (c *Connector) Check(ctx context.Context, profile string, timeout time.Duration) CheckResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := c.connect(ctx, profile, false)
	if err != nil {
		var ce *connectionError
		if errors.As(err, &ce) {
			switch ce.stage {
			case "forwarding":
				return CheckResult{Stage: "forwarding", Message: "Managed forwarding did not become ready. Check kubectl, kubeContext, serviceNamespace, service, remotePort and Kubernetes access."}
			case "TLS/config":
				return CheckResult{Stage: "TLS", Message: "TLS configuration could not be loaded. Check that caFile is a readable PEM CA bundle."}
			}
		}
		return CheckResult{Stage: "config", Message: "Profile validation failed. Check the profile name, endpoint, namespace, authMode, token source and CA file; run micko --setup to add a profile."}
	}
	defer conn.Close()
	_, err = conn.Reader.List(ctx, core.Query{Namespace: conn.Namespace, Limit: 1, SummaryOnly: true})
	if err == nil {
		return CheckResult{OK: true, Stage: "ready", Message: "Workflow list access succeeded. This checks reads in the configured namespace only; it does not verify write permissions."}
	}
	ae := core.AsAPIError(err)
	if ae != nil {
		switch {
		case ae.Kind == core.ErrUnauthenticated:
			return CheckResult{Stage: "authentication", Message: "Authentication failed (401, API login interception, or unreadable token source). Check the server auth mode and token source, expiry and audience. Browser login alone does not authenticate micko."}
		case ae.Kind == core.ErrForbidden:
			return CheckResult{Stage: "permission", Message: "The endpoint responded but denied workflow list access (403). Check the namespace and the identity's RBAC permissions."}
		case ae.Status != 0:
			return CheckResult{Stage: "API", Message: "The endpoint responded with an API error. Check the Argo base path, namespace and server health."}
		case ae.Kind == core.ErrUnavailable && strings.HasPrefix(ae.Message, "TLS handshake"):
			return CheckResult{Stage: "TLS", Message: "TLS verification or handshake failed. Check https, certificate hostname and validity, and caFile. Managed forwards use the loopback address for TLS verification."}
		case ae.Kind != core.ErrUnavailable:
			return CheckResult{Stage: "API", Message: "The endpoint did not return usable workflow API data. Check the Argo base path and proxy; redirects are refused."}
		}
	}
	if ctx.Err() != nil {
		return CheckResult{Stage: "endpoint", Message: "The check timed out or was canceled. Check endpoint reachability or forwarding; increase --check-timeout if needed."}
	}
	return CheckResult{Stage: "endpoint", Message: "The endpoint could not be reached or the response ended early. Check the URL, DNS, network, proxy and forwarding."}
}
