package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// New history kinds use the existing instance metadata store because the
// unchanged source-record table constrains its kinds to todo/channel/delivery.
// Keys include owner, source and record kind. They contain no transcript body.
type ownerSources struct {
	store *SourceRecordStore
	owner ports.Principal
}

var _ ports.ScopedSourceRecordStore = (*SourceRecordStore)(nil)

func (s *SourceRecordStore) ForOwner(owner ports.Principal) ports.SourceRecordStore {
	return &ownerSources{store: s, owner: owner}
}
func (s *ownerSources) Fingerprints(ctx context.Context, source string, ids []string) (map[string]string, error) {
	return s.lookup(ctx, source, ids, false)
}
func (s *ownerSources) Targets(ctx context.Context, source string, ids []string) (map[string]string, error) {
	return s.lookup(ctx, source, ids, true)
}
func (s *ownerSources) lookup(ctx context.Context, source string, ids []string, targets bool) (map[string]string, error) {
	values := map[string]string{}
	if len(ids) == 0 {
		return values, nil
	}
	exec := database.ExecutorFromContextOr(ctx, s.store.pool)
	namespace := domain.OwnerSourceNamespace(s.owner.WorkspaceID, s.owner.UserID, source)
	// Only owner-bound identities can suppress a restore. Legacy bundles are
	// readable, and receive scoped identities when first imported by this version.
	column := "content_fingerprint"
	if targets {
		column = "target_id"
	}
	rows, err := exec.Query(ctx, `select source_record_id,`+column+` from portability.portability_source_records where source_record_id=any($1) and source_instance_id=$2 and workspace_id=$3`, ids, namespace, s.owner.WorkspaceID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, value string
		if err = rows.Scan(&id, &value); err != nil {
			rows.Close()
			return nil, err
		}
		values[source+":"+id] = value
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if !targets {
		legacy, err := exec.Query(ctx, `select source_record_id from portability.portability_source_records where source_record_id=any($1) and source_instance_id=$2 and workspace_id=$3`, ids, source, s.owner.WorkspaceID)
		if err != nil {
			return nil, err
		}
		for legacy.Next() {
			var id string
			if err := legacy.Scan(&id); err != nil {
				legacy.Close()
				return nil, err
			}
			key := source + ":" + id
			if _, ok := values[key]; !ok {
				values[key] = "legacy-owner-unverified"
			}
		}
		err = legacy.Err()
		legacy.Close()
		if err != nil {
			return nil, err
		}
	}

	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, historySourceKey(namespace, id))
	}
	rows, err = exec.Query(ctx, `select value from public.instance_meta where key=any($1)`, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var value string
		var r dto.SourceRecord
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(value), &r); err != nil {
			return nil, err
		}
		v := r.ContentFingerprint
		if targets {
			v = r.TargetID
		}
		values[source+":"+r.SourceRecordID] = v
	}
	return values, rows.Err()
}
func historySourceKey(namespace, id string) string {
	return "portability.history:" + namespace + ":" + id
}
func (s *ownerSources) Register(ctx context.Context, r dto.SourceRecord) error {
	if r.WorkspaceID != s.owner.WorkspaceID || s.owner.UserID == "" {
		return fmt.Errorf("%w: source owner mismatch", domain.ErrRecordInvalid)
	}
	namespace := domain.OwnerSourceNamespace(s.owner.WorkspaceID, s.owner.UserID, r.SourceInstanceID)
	if r.TargetKind != domain.KindSession && r.TargetKind != domain.KindMessage {
		r.SourceInstanceID = namespace
		return s.store.Register(ctx, r)
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tag, err := database.ExecutorFromContextOr(ctx, s.store.pool).Exec(ctx, `insert into public.instance_meta(key,value) values($1,$2) on conflict(key) do nothing`, historySourceKey(namespace, r.SourceRecordID), string(data))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrSourceRecordExists
	}
	return nil
}
