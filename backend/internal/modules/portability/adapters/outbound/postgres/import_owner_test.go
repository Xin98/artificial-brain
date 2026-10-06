package postgres

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
	"testing"
)

func TestImportStoreSavesOwnerWithUploadAtomically(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewImportStore(pool)
	row := newImportRow(randomID(t), []byte("bundle"), nil)
	row.UserID = randomID(t)
	if err := store.Save(ctx, row); err != nil {
		t.Fatal(err)
	}
	restored, err := store.Get(ctx, row.WorkspaceID, row.ID)
	if err != nil || restored.UserID != row.UserID {
		t.Fatalf("uploader identity = %#v,%v", restored, err)
	}
	conflicting := newImportRow(row.WorkspaceID, []byte("bundle"), nil)
	conflicting.UserID = row.UserID
	if _, err := pool.Exec(ctx, `insert into public.instance_meta(key,value) values($1,$2)`, "portability.import-owner:"+conflicting.ID, "existing"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, conflicting); err == nil {
		t.Fatal("owner conflict accepted")
	}
	if _, err := store.Get(ctx, conflicting.WorkspaceID, conflicting.ID); !errors.Is(err, domain.ErrImportNotFound) {
		t.Fatalf("partial upload survived failed owner write: %v", err)
	}
}
