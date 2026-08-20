package dto

import "time"

// DeliveryCounts buckets a workspace's deliveries by lifecycle state.
// Sending counts sending deliveries on their first attempt; Retrying counts
// sending deliveries with at least one retry (Sending = sending∧attempt=0,
// Retrying = sending∧attempt>0).
type DeliveryCounts struct {
	Scheduled  int
	Sending    int
	Retrying   int
	Succeeded  int
	Failed     int
	Suppressed int
}

// DeliveryFilter narrows a delivery listing.
type DeliveryFilter struct {
	Status string
	Limit  int
	Offset int
}

// TodoView is the narrow slice of a todo the reminder execution path re-reads
// at send time. Status carries the todo module's status string; a stale
// ReminderVersion (newer than the plan's) means the todo was rescheduled and
// the delivery must be suppressed.
type TodoView struct {
	Title           string
	Status          string
	ReminderVersion int
	OwnerUserID     string
}

// ChannelEndpoint resolves where a reminder should be sent for one user and
// channel. Usable is false when the address is missing, unverified, or
// disabled; callers suppress rather than send.
type ChannelEndpoint struct {
	Address string
	Usable  bool
}

// ReminderMessage is the provider-agnostic payload handed to a notifier.
// TodoID identifies the todo the reminder belongs to: the fake outbox records
// it so the gated dev inbox can join back to the todo; real providers ignore
// it.
type ReminderMessage struct {
	To             string
	TodoID         string
	Title          string
	ScheduledAtUTC time.Time
}

// SendResult reports what the provider returned for an accepted send.
type SendResult struct {
	ProviderMessageID string
}

// ReceiptPayload is the provider delivery-receipt verdict parsed from a
// webhook before it is handed to the record-receipt command.
type ReceiptPayload struct {
	ProviderMessageID string
	Delivered         bool
	ErrorCode         string
}

// QueueDepth reports one delivery queue's backlog and its oldest waiting job.
type QueueDepth struct {
	Queue             string
	Depth             int
	OldestWaitSeconds int
}

// OpsView is the instance-wide operational snapshot for the reminder ops
// endpoint. It is deliberately not workspace-scoped: it is operational data.
type OpsView struct {
	Queues       []QueueDepth
	Deliveries   DeliveryCounts
	RetryRate    float64
	LatencyP95Ms int
	CheckedAt    time.Time
}
