package argo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
)

var _ core.CronLister = (*Client)(nil)

// cronPathPrefix is the cron workflow list route,
// GET /api/v1/cron-workflows/{namespace}. With an empty namespace the path is
// /api/v1/cron-workflows/ and the server lists every namespace the token may
// read, the same way the workflow list does.
const cronPathPrefix = "/api/v1/cron-workflows/"

// rawCronWorkflow is the consumed projection of one CronWorkflow.
type rawCronWorkflow struct {
	Metadata rawObjectMeta `json:"metadata"`
	Spec     struct {
		// Schedule is the v3.5 single expression; Schedules the v3.6+ list.
		Schedule                   string   `json:"schedule"`
		Schedules                  []string `json:"schedules"`
		Timezone                   string   `json:"timezone"`
		Suspend                    bool     `json:"suspend"`
		ConcurrencyPolicy          string   `json:"concurrencyPolicy"`
		StartingDeadlineSeconds    *int64   `json:"startingDeadlineSeconds"`
		SuccessfulJobsHistoryLimit *int64   `json:"successfulJobsHistoryLimit"`
		FailedJobsHistoryLimit     *int64   `json:"failedJobsHistoryLimit"`
		When                       string   `json:"when"`
		StopStrategy               *struct {
			Expression string `json:"expression"`
		} `json:"stopStrategy"`
		WorkflowSpec struct {
			Entrypoint          string           `json:"entrypoint"`
			Arguments           rawSpecArguments `json:"arguments"`
			WorkflowTemplateRef *struct {
				Name         string `json:"name"`
				ClusterScope bool   `json:"clusterScope"`
			} `json:"workflowTemplateRef"`
		} `json:"workflowSpec"`
	} `json:"spec"`
	Status struct {
		Active []struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
			UID       string `json:"uid"`
		} `json:"active"`
		LastScheduledTime *time.Time `json:"lastScheduledTime"`
		Phase             string     `json:"phase"`
		Succeeded         *int64     `json:"succeeded"`
		Failed            *int64     `json:"failed"`
		Conditions        []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"conditions"`
	} `json:"status"`
}

// ListCronWorkflows implements core.CronLister.
func (c *Client) ListCronWorkflows(ctx context.Context, namespace string) ([]core.CronWorkflow, error) {
	ctx, cancel := c.withUnaryDeadline(ctx)
	defer cancel()
	if namespace == "" {
		if err := c.checkClusterScope(ctx); err != nil {
			return nil, err
		}
	}
	var out []core.CronWorkflow
	err := c.listPages(ctx, kindPath(cronPathPrefix, namespace), "cron workflow list", func(body []byte) (string, error) {
		var env kindListEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			return "", err
		}
		for _, raw := range env.Items {
			cw, err := decodeCronWorkflow(raw)
			if err != nil {
				return "", err
			}
			out = append(out, cw)
		}
		return env.Metadata.Continue, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// decodeCronWorkflow maps one object, keeping its raw bytes for the raw view.
func decodeCronWorkflow(raw json.RawMessage) (core.CronWorkflow, error) {
	var w rawCronWorkflow
	if err := json.Unmarshal(raw, &w); err != nil {
		return core.CronWorkflow{}, err
	}
	cw := core.CronWorkflow{
		Namespace:                  w.Metadata.Namespace,
		Name:                       w.Metadata.Name,
		UID:                        w.Metadata.UID,
		CreatedAt:                  tsOrZero(w.Metadata.CreationTimestamp),
		Labels:                     w.Metadata.Labels,
		Schedules:                  mergeSchedules(w.Spec.Schedule, w.Spec.Schedules),
		Timezone:                   w.Spec.Timezone,
		Suspend:                    w.Spec.Suspend,
		ConcurrencyPolicy:          w.Spec.ConcurrencyPolicy,
		StartingDeadlineSeconds:    w.Spec.StartingDeadlineSeconds,
		SuccessfulJobsHistoryLimit: w.Spec.SuccessfulJobsHistoryLimit,
		FailedJobsHistoryLimit:     w.Spec.FailedJobsHistoryLimit,
		When:                       w.Spec.When,
		Entrypoint:                 w.Spec.WorkflowSpec.Entrypoint,
		Arguments:                  arguments(w.Spec.WorkflowSpec.Arguments.Parameters),
		LastScheduledTime:          w.Status.LastScheduledTime,
		Phase:                      w.Status.Phase,
		Succeeded:                  w.Status.Succeeded,
		Failed:                     w.Status.Failed,
		Resource:                   append(json.RawMessage(nil), raw...),
	}
	if w.Spec.StopStrategy != nil {
		cw.StopExpression = w.Spec.StopStrategy.Expression
	}
	if ref := w.Spec.WorkflowSpec.WorkflowTemplateRef; ref != nil && ref.Name != "" {
		cw.WorkflowTemplateRef = ref.Name
		if ref.ClusterScope {
			cw.WorkflowTemplateRef = "cluster/" + ref.Name
		}
	}
	for _, a := range w.Status.Active {
		cw.Active = append(cw.Active, core.Ref{Namespace: a.Namespace, Name: a.Name, UID: a.UID})
	}
	for _, cond := range w.Status.Conditions {
		cw.Conditions = append(cw.Conditions, core.Condition{Type: cond.Type, Status: cond.Status, Message: cond.Message})
	}
	return cw, nil
}

// mergeSchedules folds the two schedule fields into one list. A v3.6 object
// can carry both while it moves from one to the other; the single field
// comes first and a duplicate is listed once.
func mergeSchedules(single string, list []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(single)
	for _, s := range list {
		add(s)
	}
	return out
}
