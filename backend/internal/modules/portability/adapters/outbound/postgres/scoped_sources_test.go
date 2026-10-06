package postgres

import (
	"context"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
)

func TestScopedConversationSourcesStayIdempotentAndPrivate(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewSourceRecordStore(pool)
	p := ports.Principal{WorkspaceID: randomID(t), UserID: randomID(t)}
	owner := store.ForOwner(p)
	record := dto.SourceRecord{WorkspaceID: p.WorkspaceID, SourceInstanceID: "bundle-origin", SourceRecordID: "session:source", TargetKind: "session", TargetID: randomID(t), ContentFingerprint: "same"}
	if err := owner.Register(ctx, record); err != nil {
		t.Fatal(err)
	}
	fingerprints, err := owner.Fingerprints(ctx, "bundle-origin", []string{"session:source"})
	if err != nil || fingerprints["bundle-origin:session:source"] != "same" {
		t.Fatalf("own source = %#v, %v", fingerprints, err)
	}
	for _, foreign := range []ports.Principal{{WorkspaceID: p.WorkspaceID, UserID: randomID(t)}, {WorkspaceID: randomID(t), UserID: p.UserID}} {
		scoped := store.ForOwner(foreign)
		fingerprints, err = scoped.Fingerprints(ctx, "bundle-origin", []string{"session:source"})
		if err != nil || len(fingerprints) != 0 {
			t.Fatalf("foreign fingerprints leaked = %#v,%v", fingerprints, err)
		}
		record.WorkspaceID = foreign.WorkspaceID
		if err := scoped.Register(ctx, record); err != nil {
			t.Fatalf("foreign restore collided: %v", err)
		}
	}
}

func TestScopedLegacySourceNeedsOwnerProofOnlyInItsWorkspace(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewSourceRecordStore(pool)
	workspace := randomID(t)
	if err := store.Register(ctx, dto.SourceRecord{WorkspaceID: workspace, SourceInstanceID: "legacy", SourceRecordID: "todo", TargetKind: "todo", TargetID: randomID(t), ContentFingerprint: "same"}); err != nil {
		t.Fatal(err)
	}
	same, err := store.ForOwner(ports.Principal{WorkspaceID: workspace, UserID: randomID(t)}).Fingerprints(ctx, "legacy", []string{"todo"})
	if err != nil || same["legacy:todo"] != "legacy-owner-unverified" {
		t.Fatalf("legacy owner uncertainty = %#v,%v", same, err)
	}
	foreign, err := store.ForOwner(ports.Principal{WorkspaceID: randomID(t), UserID: randomID(t)}).Fingerprints(ctx, "legacy", []string{"todo"})
	if err != nil || len(foreign) != 0 {
		t.Fatalf("foreign legacy identity suppressed restore: %#v,%v", foreign, err)
	}
}
