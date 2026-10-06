package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

type ownerImportStore struct{ row dto.ImportRecordRow }

func (s *ownerImportStore) Save(context.Context, dto.ImportRecordRow) error { return nil }
func (s *ownerImportStore) Get(context.Context, string, string) (dto.ImportRecordRow, error) {
	return s.row, nil
}
func (s *ownerImportStore) Commit(context.Context, string, string, dto.ImportReport, time.Time) error {
	return nil
}
func TestGetImportOwnerBoundaryIncludesLegacyRows(t *testing.T) {
	store := &ownerImportStore{row: dto.ImportRecordRow{ID: "import", WorkspaceID: "workspace", UserID: "owner", CreatedAt: time.Now()}}
	q := GetImportQuery{Imports: store, Now: time.Now, ImportTTL: time.Hour}
	if _, err := q.HandleForOwner(context.Background(), ports.Principal{WorkspaceID: "workspace", UserID: "owner"}, "import"); err != nil {
		t.Fatal(err)
	}
	for _, uploader := range []string{"another", ""} {
		store.row.UserID = uploader
		if _, err := q.HandleForOwner(context.Background(), ports.Principal{WorkspaceID: "workspace", UserID: "owner"}, "import"); !errors.Is(err, domain.ErrImportNotFound) {
			t.Fatalf("unproven owner exposed bundle: %v", err)
		}
	}
}
