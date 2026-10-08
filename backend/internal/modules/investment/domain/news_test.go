package domain

import (
	"testing"
	"time"
)

func TestTopicWindowAndArticleDedup(t *testing.T) {
	now := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
	item := func(id, title string, ago time.Duration) NewsItem {
		return NewsItem{ID: id, Title: title, PublishedAt: now.Add(-ago), AvailableAt: now.Add(-ago)}
	}
	topics, e := AggregateTopics([]NewsItem{item("a", "Earnings beat", time.Hour), item("a", "Earnings beat", time.Hour), item("b", "Earnings miss", 30*time.Hour), item("old", "Earnings old", 8*24*time.Hour), item("future", "Earnings future", -time.Hour)}, now)
	if e != nil || len(topics) != 1 || topics[0].Count7Days != 2 || topics[0].Count24Hours != 1 || topics[0].Previous24Hours != 1 {
		t.Fatal(topics, e)
	}
}
