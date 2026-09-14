package argo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"

	"argo-tui/internal/core"
)

var _ core.NamespaceLister = (*Client)(nil)

// namespaces.go answers "which namespaces can I look at?".
//
// Argo Workflows has no endpoint that lists namespaces, so there is nothing to
// call directly and nothing to guess from. Two facts the server will state are
// combined instead:
//
//  1. GET /api/v1/info returns managedNamespace when the server was started
//     for one namespace. Then that is the only answer there can be, and no
//     further request is worth making.
//
//  2. Otherwise the workflow list is requested with no namespace in the path.
//     The server answers with the workflows this token may read across the
//     cluster, and their own metadata.namespace is the list. It needs no
//     permission beyond the one argo-tui already uses to show workflows.
//
// The second path is honest but partial: a namespace with no workflows in the
// returned page cannot appear in it. The note says so, and the picker always
// accepts a typed name, so a quiet namespace is never unreachable.

// namespaceScanLimit bounds the list used to derive namespaces. It is a
// single page: this runs when the reader opens the picker and must answer in
// the time it takes them to read the pane, not walk every workflow on the
// cluster.
const namespaceScanLimit = 500

// infoResponse is the consumed part of GET /api/v1/info.
type infoResponse struct {
	ManagedNamespace string `json:"managedNamespace"`
}

// namespaceEnvelope is the consumed projection of a cluster-wide list.
type namespaceEnvelope struct {
	Items []struct {
		Metadata struct {
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	} `json:"items"`
}

// ListNamespaces implements core.NamespaceLister.
func (c *Client) ListNamespaces(ctx context.Context) ([]string, string, error) {
	ctx, cancel := c.withUnaryDeadline(ctx)
	defer cancel()
	if ns, err := c.managedNamespace(ctx); err == nil && ns != "" {
		return []string{ns}, "the server manages this namespace only", nil
	}
	req, err := c.newRequest(ctx, "/api/v1/workflows/", url.Values{
		"listOptions.limit": []string{itoaNS(namespaceScanLimit)},
		// A projection keeps the scan cheap: the node map of every workflow
		// on the cluster is a large payload to download for one field.
		"fields": []string{"items.metadata.namespace"},
	})
	if err != nil {
		return nil, "", err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, "", err
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, "", mapHTTPError(resp, http.MethodGet, "/api/v1/workflows/", readBody(resp.Body), nil, c.now)
	}
	body, err := readAllBody(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if isHTMLBody(body, resp) {
		return nil, "", mapHTTPError(resp, http.MethodGet, "/api/v1/workflows/", body, nil, c.now)
	}
	var env namespaceEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, "", core.WrapAPIError(core.ErrProtocol, 0, "namespaces: unparseable list response", err)
	}
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	for _, it := range env.Items {
		ns := sanitizeLine(it.Metadata.Namespace)
		if ns == "" || seen[ns] {
			continue
		}
		seen[ns] = true
		out = append(out, ns)
	}
	sort.Strings(out)
	return out, "read from the workflows this token can see", nil
}

// managedNamespace asks the server whether it is scoped to one namespace. A
// failure is not an error for the caller: it only means this shortcut is
// unavailable and the list has to be derived instead.
func (c *Client) managedNamespace(ctx context.Context) (string, error) {
	req, err := c.newRequest(ctx, "/api/v1/info", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return "", err
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", mapHTTPError(resp, http.MethodGet, "/api/v1/info", readBody(resp.Body), nil, c.now)
	}
	body, err := readAllBody(resp.Body)
	if err != nil {
		return "", err
	}
	if isHTMLBody(body, resp) {
		return "", mapHTTPError(resp, http.MethodGet, "/api/v1/info", body, nil, c.now)
	}
	var info infoResponse
	if err := json.Unmarshal(body, &info); err != nil {
		return "", core.WrapAPIError(core.ErrProtocol, 0, "info: unparseable response", err)
	}
	return sanitizeLine(info.ManagedNamespace), nil
}

func itoaNS(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
