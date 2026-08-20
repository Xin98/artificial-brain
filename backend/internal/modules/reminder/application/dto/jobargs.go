package dto

// ReminderSendArgs is the payload persisted on each River job that delivers
// one channel of one planned reminder. It is a plain struct on purpose: the
// application layer must not import the queue, so the River adapter wraps it
// while the worker adapter decodes it. Every execution-time read the send
// command performs is scoped by WorkspaceID and OwnerUserID, so both travel
// with the job.
type ReminderSendArgs struct {
	PlanID              string `json:"plan_id"`
	WorkspaceID         string `json:"workspace_id"`
	OwnerUserID         string `json:"owner_user_id"`
	TodoID              string `json:"todo_id"`
	TodoReminderVersion int    `json:"todo_reminder_version"`
	Channel             string `json:"channel"`
}

// Kind identifies the job type in the queue. Jobs are keyed by this string
// rather than a Go type name so workers keep matching jobs across deploys and
// renames.
func (ReminderSendArgs) Kind() string { return "reminder_send" }
