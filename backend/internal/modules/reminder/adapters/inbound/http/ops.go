package http

import (
	"net/http"
	"time"

	reminderdto "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
)

// queueDepthView is the wire shape of one delivery queue's backlog.
type queueDepthView struct {
	Queue             string `json:"queue"`
	Depth             int    `json:"depth"`
	OldestWaitSeconds int    `json:"oldestWaitSeconds"`
}

// deliveryCountsView is the wire shape of the delivery lifecycle counts.
type deliveryCountsView struct {
	Scheduled  int `json:"scheduled"`
	Sending    int `json:"sending"`
	Retrying   int `json:"retrying"`
	Succeeded  int `json:"succeeded"`
	Failed     int `json:"failed"`
	Suppressed int `json:"suppressed"`
}

// reminderOpsView is the wire shape of the instance-wide reminder ops
// snapshot. It is operational data and deliberately not workspace-scoped.
type reminderOpsView struct {
	Queues       []queueDepthView   `json:"queues"`
	Deliveries   deliveryCountsView `json:"deliveries"`
	RetryRate    float64            `json:"retryRate"`
	LatencyP95Ms int                `json:"latencyP95Ms"`
	CheckedAt    time.Time          `json:"checkedAt"`
}

// reminderOps serves GET /api/v1/ops/reminder: the instance-wide reminder
// operations snapshot.
func (h *Handler) reminderOps(w http.ResponseWriter, r *http.Request) {
	if _, ok := principalFrom(w, r); !ok {
		return
	}
	view, err := h.Ops.Handle(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, newReminderOpsView(view))
}

func newReminderOpsView(view reminderdto.OpsView) reminderOpsView {
	queues := make([]queueDepthView, 0, len(view.Queues))
	for _, queue := range view.Queues {
		queues = append(queues, queueDepthView{
			Queue:             queue.Queue,
			Depth:             queue.Depth,
			OldestWaitSeconds: queue.OldestWaitSeconds,
		})
	}
	return reminderOpsView{
		Queues: queues,
		Deliveries: deliveryCountsView{
			Scheduled:  view.Deliveries.Scheduled,
			Sending:    view.Deliveries.Sending,
			Retrying:   view.Deliveries.Retrying,
			Succeeded:  view.Deliveries.Succeeded,
			Failed:     view.Deliveries.Failed,
			Suppressed: view.Deliveries.Suppressed,
		},
		RetryRate:    view.RetryRate,
		LatencyP95Ms: view.LatencyP95Ms,
		CheckedAt:    view.CheckedAt,
	}
}
