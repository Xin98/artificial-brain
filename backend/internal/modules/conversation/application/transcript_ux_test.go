package application

import (
	"strings"
	"testing"
	"time"

	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
)

func TestCreateSummaryMakesUndatedReminderStateExplicit(t *testing.T) {
	got := CreateSchedulingSummary(tododto.Todo{Title: "采购"}, "", "", nil)
	if !strings.Contains(got, "未设置到期时间，未安排提醒") {
		t.Fatalf("undated summary = %q", got)
	}
}

func TestCreateSummaryIncludesScheduledChannelSnapshot(t *testing.T) {
	scheduled, channels, due := true, []string{"email"}, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	got := CreateSchedulingSummary(tododto.Todo{Title: "采购", ReminderScheduled: &scheduled, ReminderChannels: &channels}, "", "", &due)
	if !strings.Contains(got, "已安排提醒（email）") || strings.Contains(got, "未安排提醒") {
		t.Fatalf("scheduled summary = %q", got)
	}
}
