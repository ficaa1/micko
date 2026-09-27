package argo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// kinds.go holds what the resource kinds beside workflows share on the wire:
// a paged list request and the parts of a workflow spec they embed.
//
// Cron workflows and templates are listed whole. Their list endpoints take
// no `fields` projection (the swagger declares none for them, unlike the
// workflow list), and an object without a node map is small, so a page of
// them costs what a page of workflow summaries costs.

// kindPageSize is the page each kind list asks for.
const kindPageSize = 100

// kindMaxPages bounds one collection. A continuation token that never ends
// would otherwise hold the refresh forever; past this bound the list is
// refused with a message rather than shown incomplete as if whole.
const kindMaxPages = 100

// listPages collects a paged list from path. decode reads one page body and
// returns its continuation token. A namespaced path whose namespace segment
// is empty lists every namespace; the caller checks the server's scope first.
func (c *Client) listPages(ctx context.Context, path, what string, decode func(body []byte) (string, error)) error {
	cont := ""
	for pages := 0; ; pages++ {
		if pages >= kindMaxPages {
			return core.ErrProtocalf("%s: more than %d pages; narrow the list to one namespace", what, kindMaxPages)
		}
		query := url.Values{}
		query.Set("listOptions.limit", fmt.Sprintf("%d", kindPageSize))
		if cont != "" {
			query.Set("listOptions.continue", cont)
		}
		body, err := c.getBody(ctx, path, query)
		if err != nil {
			return err
		}
		next, err := decode(body)
		if err != nil {
			return core.WrapAPIError(core.ErrProtocol, 0, what+": unparseable response (protocol mismatch)", err)
		}
		if next == "" || next == cont {
			return nil
		}
		cont = next
	}
}

// getBody performs one bounded GET and returns the body of a 200 answer. Every
// other answer, and a login page served as 200, becomes a typed error.
func (c *Client) getBody(ctx context.Context, path string, query url.Values) ([]byte, error) {
	req, err := c.newRequest(ctx, path, query)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, mapHTTPError(resp, http.MethodGet, path, readBody(resp.Body), nil, c.now)
	}
	body, err := readAllBody(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, core.ErrProtocalf("GET %s: empty response body", path)
	}
	if isHTMLBody(body, resp) {
		return nil, mapHTTPError(resp, http.MethodGet, path, body, nil, c.now)
	}
	return body, nil
}

// kindListEnvelope is the list shape every kind shares: continuation in the
// list metadata, and the items kept raw so each can be decoded and also
// preserved verbatim for the raw view.
type kindListEnvelope struct {
	Metadata struct {
		Continue string `json:"continue"`
	} `json:"metadata"`
	Items []json.RawMessage `json:"items"`
}

// rawObjectMeta is the consumed part of an object's metadata.
type rawObjectMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	Labels            map[string]string `json:"labels"`
	CreationTimestamp *time.Time        `json:"creationTimestamp"`
}

// rawSpecArguments is a workflow spec's arguments block.
type rawSpecArguments struct {
	Parameters []rawArgument `json:"parameters"`
}

// rawArgument is one argument parameter. value, default and the enum entries
// are Argo's AnyString: a string on the wire from the server, but a number or
// a boolean in an object written by hand and stored as such, so they are
// read raw and rendered as their text.
type rawArgument struct {
	Name        string            `json:"name"`
	Value       json.RawMessage   `json:"value"`
	Default     json.RawMessage   `json:"default"`
	Enum        []json.RawMessage `json:"enum"`
	Description string            `json:"description"`
	ValueFrom   *struct {
		ConfigMapKeyRef *struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"configMapKeyRef"`
		Expression string `json:"expression"`
		Parameter  string `json:"parameter"`
		Path       string `json:"path"`
		JSONPath   string `json:"jsonPath"`
		Event      string `json:"event"`
		Supplied   *struct {
		} `json:"supplied"`
	} `json:"valueFrom"`
}

// anyString renders an AnyString value. ok is false when the field is absent
// or null.
func anyString(raw json.RawMessage) (string, bool) {
	t := strings.TrimSpace(string(raw))
	if t == "" || t == "null" {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	return t, true
}

// arguments maps the wire parameters onto the core shape.
func arguments(ps []rawArgument) []core.Argument {
	if len(ps) == 0 {
		return nil
	}
	out := make([]core.Argument, 0, len(ps))
	for _, p := range ps {
		a := core.Argument{Name: p.Name, Description: p.Description}
		a.Value, a.HasValue = anyString(p.Value)
		a.Default, _ = anyString(p.Default)
		for _, e := range p.Enum {
			if v, ok := anyString(e); ok {
				a.Enum = append(a.Enum, v)
			}
		}
		a.ValueFrom = valueFromText(p)
		out = append(out, a)
	}
	return out
}

// valueFromText names the source of a parameter read at run time.
func valueFromText(p rawArgument) string {
	vf := p.ValueFrom
	if vf == nil {
		return ""
	}
	switch {
	case vf.ConfigMapKeyRef != nil:
		return "configmap " + vf.ConfigMapKeyRef.Name + " key " + vf.ConfigMapKeyRef.Key
	case vf.Expression != "":
		return "expression " + vf.Expression
	case vf.Parameter != "":
		return "parameter " + vf.Parameter
	case vf.Event != "":
		return "event " + vf.Event
	case vf.JSONPath != "":
		return "jsonPath " + vf.JSONPath
	case vf.Path != "":
		return "path " + vf.Path
	case vf.Supplied != nil:
		return "supplied at run time"
	default:
		return "valueFrom"
	}
}

// kindPath builds a namespaced list path. An empty namespace keeps the
// trailing slash, which Argo's route matches with an empty namespace segment.
func kindPath(prefix, namespace string) string {
	return prefix + url.PathEscape(namespace)
}
