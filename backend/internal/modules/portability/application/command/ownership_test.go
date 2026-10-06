package command

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
	"testing"
)

func TestImportCannotConfirmAnotherUserUpload(t *testing.T) {
	rig := newConfirmRig()
	row := rig.seedPendingImport([]byte("private bundle"))
	row.UserID = "another-user"
	rig.imports.rows[importRowKey(row.WorkspaceID, row.ID)] = row
	_, err := rig.handler.Handle(context.Background(), testPrincipal(), row.ID)
	if !errors.Is(err, domain.ErrImportNotFound) || rig.parser.calls != 0 {
		t.Fatalf("foreign upload accepted: %v", err)
	}
}
