// Package kube reads what profile setup offers from the kubeconfig and the
// cluster: contexts, Argo Server services and namespaces. It only reads.
package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

// Run executes kubectl with args and returns its standard output.
type Run func(ctx context.Context, args ...string) ([]byte, error)

// Kubectl runs the kubectl on PATH. A failure carries kubectl's first
// stderr line, which names the problem (no such context, forbidden).
func Kubectl(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "kubectl", args...).Output()
	if errors.Is(err, exec.ErrNotFound) {
		return nil, errors.New("kubectl is not on PATH; setup reads the kubeconfig with it")
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if line, _, _ := strings.Cut(strings.TrimSpace(string(exit.Stderr)), "\n"); line != "" {
			return nil, errors.New(line)
		}
	}
	return out, err
}

// Contexts lists the kubeconfig's contexts and names the current one, which
// is empty when none is set.
func Contexts(ctx context.Context, run Run) ([]string, string, error) {
	out, err := run(ctx, "config", "get-contexts", "-o", "name")
	if err != nil {
		return nil, "", err
	}
	names := strings.Fields(string(out))
	current, _ := run(ctx, "config", "current-context")
	return names, strings.TrimSpace(string(current)), nil
}

// Service is a Kubernetes service that looks like an Argo Server.
type Service struct {
	Namespace, Name string
	Port            int
}

// ArgoServers lists the services in every namespace of kubeContext that the
// Argo manifests or Helm chart create for the Argo Server.
func ArgoServers(ctx context.Context, run Run, kubeContext string) ([]Service, error) {
	out, err := run(ctx, "--context", kubeContext, "get", "services", "--all-namespaces", "-o", "json")
	if err != nil {
		return nil, err
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name      string            `json:"name"`
				Namespace string            `json:"namespace"`
				Labels    map[string]string `json:"labels"`
			} `json:"metadata"`
			Spec struct {
				Ports []struct {
					Name string `json:"name"`
					Port int    `json:"port"`
				} `json:"ports"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.NewDecoder(bytes.NewReader(out)).Decode(&list); err != nil {
		return nil, fmt.Errorf("kubectl get services: unreadable output")
	}
	var found []Service
	for _, it := range list.Items {
		md := it.Metadata
		l := md.Labels
		server := l["app"] == "argo-server" ||
			l["app.kubernetes.io/name"] == "argo-workflows-server" ||
			(l["app.kubernetes.io/part-of"] == "argo-workflows" && l["app.kubernetes.io/component"] == "server") ||
			strings.HasSuffix(md.Name, "argo-server") || strings.HasSuffix(md.Name, "workflows-server")
		if !server || len(it.Spec.Ports) == 0 {
			continue
		}
		port := it.Spec.Ports[0].Port
		for _, p := range it.Spec.Ports {
			if p.Port == 2746 || p.Name == "web" {
				port = p.Port
				break
			}
		}
		found = append(found, Service{Namespace: md.Namespace, Name: md.Name, Port: port})
	}
	slices.SortFunc(found, func(a, b Service) int {
		return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})
	return found, nil
}

// Namespaces lists kubeContext's namespaces, sorted.
func Namespaces(ctx context.Context, run Run, kubeContext string) ([]string, error) {
	out, err := run(ctx, "--context", kubeContext, "get", "namespaces", "-o", "name")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, f := range strings.Fields(string(out)) {
		names = append(names, strings.TrimPrefix(f, "namespace/"))
	}
	slices.Sort(names)
	return names, nil
}
