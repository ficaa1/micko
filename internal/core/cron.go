package core

import (
	"context"
	"encoding/json"
	"time"
)

// CronWorkflow is the read projection of an Argo CronWorkflow: its schedule,
// its policy, what the controller last did with it, and the workflow it
// starts.
//
// CronWorkflow folds both schedule schemas into Schedules: v3.5's single
// spec.schedule and the spec.schedules list that replaced it. Status fields
// an older server does not report are nil or empty, never zero.
type CronWorkflow struct {
	Namespace string
	Name      string
	UID       string
	CreatedAt time.Time
	Labels    map[string]string

	// Schedules are the cron expressions as written, in object order.
	Schedules []string
	// Timezone is spec.timezone verbatim. Empty means the controller's own
	// local time.
	Timezone string
	// Suspend is spec.suspend: no new runs are started while it is set.
	Suspend bool
	// ConcurrencyPolicy is Allow, Forbid or Replace; empty means Allow.
	ConcurrencyPolicy string
	// StartingDeadlineSeconds bounds how late a missed run may still start.
	StartingDeadlineSeconds *int64
	// History limits: how many finished runs of each outcome are kept.
	SuccessfulJobsHistoryLimit *int64
	FailedJobsHistoryLimit     *int64
	// When is the v3.6 expression a run must satisfy to be started.
	When string
	// StopExpression is the v3.6 stopStrategy.expression; the controller
	// stops scheduling once it holds.
	StopExpression string

	// Entrypoint and Arguments describe the workflow each run starts.
	// Argument values can hold secrets, so views redact them unless the
	// reader reveals them.
	Entrypoint string
	Arguments  []Argument
	// WorkflowTemplateRef names the template the spec runs, when it runs
	// one instead of carrying its own templates.
	WorkflowTemplateRef string

	// Active are the runs the controller counts as still going.
	Active []Ref
	// LastScheduledTime is when the controller last started a run.
	LastScheduledTime *time.Time
	// Phase is the v3.6 status.phase: Active, or Stopped once the stop
	// expression held.
	Phase string
	// Succeeded and Failed are the v3.6 counters of finished runs.
	Succeeded *int64
	Failed    *int64
	// Conditions are the controller's reports about the object, such as a
	// SpecError for a schedule it could not parse.
	Conditions []Condition

	// Resource is the object as the server returned it, for the raw view. It
	// is parsed and redacted before rendering, never echoed raw.
	Resource json.RawMessage
}

// Argument is one entry of a workflow spec's arguments.parameters, as a
// template or a cron workflow declares it. Value and Default can hold
// secrets; views redact both unless the reader reveals them.
type Argument struct {
	Name  string
	Value string
	// HasValue separates an empty value from none: a parameter declared
	// with no value must be supplied at submission.
	HasValue bool
	Default  string
	// Enum lists the allowed values, when the parameter restricts them.
	Enum        []string
	Description string
	// ValueFrom names where the value is read from at run time (a config
	// map key, for example), when it is not given inline.
	ValueFrom string
}

// Condition is one status condition.
type Condition struct {
	Type    string
	Status  string
	Message string
}

// CronLister lists cron workflows. It is optional: a Reader that does not
// implement it has no cron view, and the palette says so.
type CronLister interface {
	// ListCronWorkflows returns every cron workflow in namespace, or in every
	// namespace the token may read when namespace is empty.
	ListCronWorkflows(ctx context.Context, namespace string) ([]CronWorkflow, error)
}
