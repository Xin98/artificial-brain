package application

import (
	"fmt"
	"strings"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
)

// Resolved-intent labels persisted on user transcript rows. Dispatched turns
// keep their proposal intent (todo.create/todo.list/todo.delete); the labels
// below cover the non-dispatched outcomes so history replay can tell them
// apart.
const (
	ResolvedIntentChat          = "chat"
	ResolvedIntentClarification = "clarification"
	ResolvedIntentUnsupported   = "unsupported"
)

// MaxHistoryBodyRunes bounds each transcript body handed back to the model
// as multi-turn context (D6 token safety).
const MaxHistoryBodyRunes = 500

// Fixed assistant copy for turns that never reach Todo.
const (
	// UnsupportedSummary answers fail-closed turns (invalid envelope or
	// invalid proposal); the model reply is deliberately NOT reused so an
	// invalid action never masquerades as chat.
	UnsupportedSummary = "这个请求暂时不支持。"
	// NotFoundSummary answers a delete whose keyword matched nothing.
	NotFoundSummary = "没有找到匹配的待办。"
)

// CreateSummary builds the deterministic assistant row for a dispatched
// todo.create: the local echo when one resolved, else the UTC instant, else
// the bare title.
func CreateSummary(title, localEcho, timezone string, dueAtUTC *time.Time) string {
	switch {
	case localEcho != "":
		return fmt.Sprintf("已创建待办「%s」，提醒时间 %s（%s）。", title, localEcho, timezone)
	case dueAtUTC != nil:
		return fmt.Sprintf("已创建待办「%s」，提醒时间 %s。", title, dueAtUTC.UTC().Format(time.RFC3339))
	default:
		return fmt.Sprintf("已创建待办「%s」。", title)
	}
}

// ListSummary builds the assistant row for a dispatched todo.list.
func ListSummary(count int) string {
	return fmt.Sprintf("已列出 %d 条待办。", count)
}

// CreateSchedulingSummary reports the committed scheduling result, keeping
// the resolved due date distinct from whether delivery was scheduled.
func CreateSchedulingSummary(todo tododto.Todo, localEcho, timezone string, dueAtUTC *time.Time) string {
	body := fmt.Sprintf("已创建待办「%s」。", todo.Title)
	if dueAtUTC == nil {
		return body + "未设置到期时间，未安排提醒。"
	}
	when := dueAtUTC.UTC().Format(time.RFC3339)
	if localEcho != "" {
		when = localEcho + "（" + timezone + "）"
	}
	body += "到期时间 " + when + "。"
	if todo.ReminderScheduled == nil || !*todo.ReminderScheduled {
		return body + "未安排提醒：没有可用的提醒渠道，请先配置并验证渠道。"
	}
	channels := ""
	if todo.ReminderChannels != nil {
		channels = strings.Join(*todo.ReminderChannels, "、")
	}
	return body + "已安排提醒（" + channels + "）；实际送达结果可在提醒页面查看。"
}

// TodoListSummary preserves the displayed result for history replay.
func TodoListSummary(todos []tododto.Todo) string {
	var body strings.Builder
	body.WriteString(ListSummary(len(todos)))
	for _, todo := range todos {
		when := "无到期时间"
		if todo.DueAtUTC != nil {
			when = todo.DueAtUTC.UTC().Format(time.RFC3339)
			if todo.TimezoneAtInput != nil {
				if loc, err := time.LoadLocation(*todo.TimezoneAtInput); err == nil {
					when = todo.DueAtUTC.In(loc).Format("2006-01-02 15:04") + "（" + *todo.TimezoneAtInput + "）"
				}
			}
		}
		fmt.Fprintf(&body, "\n- %s | %s | %s", todo.Title, todo.Status, when)
	}
	return body.String()
}

// DeletedSummary records the completed action independently of the prompt.
func DeletedSummary(title string) string { return fmt.Sprintf("已删除待办「%s」。", title) }

// ConfirmationSummary builds the assistant row for a delete that resolved to
// exactly one candidate and now awaits confirmation.
func ConfirmationSummary(title string) string {
	return fmt.Sprintf("找到待办「%s」，请在界面上确认删除。", title)
}

// CandidatesSummary builds the assistant row for a delete that resolved to
// several candidates the user must choose from.
func CandidatesSummary(count int) string {
	return fmt.Sprintf("找到 %d 条相关待办，请选择要删除的一条。", count)
}

// TruncateHistoryBody bounds one transcript row handed to the model.
func TruncateHistoryBody(body string) string {
	runes := []rune(body)
	if len(runes) > MaxHistoryBodyRunes {
		return string(runes[:MaxHistoryBodyRunes])
	}
	return body
}

// SessionView projects a domain session onto its API shape.
func SessionView(session domain.Session) dto.SessionView {
	return dto.SessionView{
		ID:        session.ID,
		Title:     session.Title,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}
}
