package kube

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fake answers kubectl by its argument line.
func fake(answers map[string]string) Run {
	return func(_ context.Context, args ...string) ([]byte, error) {
		out, ok := answers[strings.Join(args, " ")]
		if !ok {
			return nil, errors.New("error: no answer for " + strings.Join(args, " "))
		}
		return []byte(out), nil
	}
}

// Setup offers the kubeconfig's contexts with the current one named, the
// services the Argo manifests and Helm chart create, and the namespaces.
func TestDiscovery(t *testing.T) {
	services := `{"items":[
		{"metadata":{"name":"argo-server","namespace":"argo","labels":{"app":"argo-server"}},"spec":{"ports":[{"name":"web","port":2746}]}},
		{"metadata":{"name":"wf-argo-workflows-server","namespace":"ci","labels":{"app.kubernetes.io/name":"argo-workflows-server"}},"spec":{"ports":[{"name":"metrics","port":9090},{"name":"http","port":2746}]}},
		{"metadata":{"name":"custom","namespace":"ops","labels":{"app.kubernetes.io/part-of":"argo-workflows","app.kubernetes.io/component":"server"}},"spec":{"ports":[{"name":"https","port":443}]}},
		{"metadata":{"name":"workflow-controller-metrics","namespace":"argo","labels":{"app":"workflow-controller"}},"spec":{"ports":[{"port":9090}]}},
		{"metadata":{"name":"argo-server","namespace":"empty"},"spec":{"ports":[]}}
	]}`
	run := fake(map[string]string{
		"config get-contexts -o name":                              "kind-dev\nprod-eks\n",
		"config current-context":                                   "kind-dev\n",
		"--context kind-dev get services --all-namespaces -o json": services,
		"--context kind-dev get namespaces -o name":                "namespace/workflows\nnamespace/argo\n",
		"--context broken get services --all-namespaces -o json":   "not json",
	})
	ctx := context.Background()

	names, current, err := Contexts(ctx, run)
	if err != nil || !reflect.DeepEqual(names, []string{"kind-dev", "prod-eks"}) || current != "kind-dev" {
		t.Fatalf("Contexts = %v %q %v", names, current, err)
	}

	got, err := ArgoServers(ctx, run, "kind-dev")
	want := []Service{{"argo", "argo-server", 2746}, {"ci", "wf-argo-workflows-server", 2746}, {"ops", "custom", 443}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ArgoServers = %+v, %v; want %+v", got, err, want)
	}
	if _, err := ArgoServers(ctx, run, "broken"); err == nil {
		t.Fatal("unreadable kubectl output was accepted")
	}
	if _, err := ArgoServers(ctx, run, "missing"); err == nil || !strings.Contains(err.Error(), "no answer") {
		t.Fatalf("kubectl failure = %v, want its message", err)
	}

	ns, err := Namespaces(ctx, run, "kind-dev")
	if err != nil || !reflect.DeepEqual(ns, []string{"argo", "workflows"}) {
		t.Fatalf("Namespaces = %v, %v", ns, err)
	}
}
