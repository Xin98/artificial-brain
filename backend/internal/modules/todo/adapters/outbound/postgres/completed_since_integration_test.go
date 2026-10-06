package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

func TestCompletedSinceFiltersOldRowsBeforeLimitAndIncludesBoundary(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewStore(pool)
	workspace, user := randomID(t), randomID(t)
	boundary := testNow.Add(-7 * 24 * time.Hour)
	insertCompleted := func(owner, title string, created, completed time.Time) {
		t.Helper()
		row, err := domain.New(randomID(t), workspace, owner, title, nil, nil, nil, created)
		if err != nil {
			t.Fatal(err)
		}
		if err := row.Complete(1, completed); err != nil {
			t.Fatal(err)
		}
		if err := store.Insert(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 205; i++ {
		insertCompleted(user, fmt.Sprintf("old-%d", i), testNow.Add(-30*24*time.Hour), boundary.Add(-time.Second))
	}
	insertCompleted(user, "boundary", testNow.Add(-2*24*time.Hour), boundary)
	insertCompleted(user, "recent", testNow.Add(-24*time.Hour), testNow)
	insertCompleted(randomID(t), "foreign", testNow.Add(-24*time.Hour), testNow)
	got, err := store.List(ctx, workspace, user, dto.ListFilters{Status: "completed", CompletedSince: &boundary}, dto.MaxListLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "boundary" || got[1].Title != "recent" {
		t.Fatalf("recent completed rows hidden or boundary/scoping lost: %#v", got)
	}
}
