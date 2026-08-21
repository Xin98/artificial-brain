// Package command holds the Portability application's write-side handlers.
package command

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

// defaultPageSize bounds how many records each exporter page carries when the
// handler is configured without an explicit page size.
const defaultPageSize = 200

// ExportBundleHandler streams a workspace's full history — todos, reminder
// deliveries, channel preferences — into an export bundle archive.
//
// Archive contract: the ArchiveWriter records the sha256 of every entry it
// streams; WriteManifest fills the manifest's Files map (shared by reference
// — Handle always passes a non-nil map) with those digests before appending
// manifest.json. Handle always calls Close, on success and on error: on the
// error path the streaming error wins and the close error is discarded, since
// the response is aborted anyway; on the success path the close error is
// returned, because an unfinalized archive is a failed export.
type ExportBundleHandler struct {
	Instance   ports.InstanceIdentityStore
	Todos      ports.TodoExporter
	Channels   ports.ChannelExporter
	Deliveries ports.DeliveryExporter
	Archive    ports.ArchiveFactory
	PageSize   int
	Now        func() time.Time
}

// Handle resolves the instance id, streams the bundle entries in contract
// order — todos.json, reminder-deliveries.json, preferences.json, todos.csv —
// then finalizes with manifest.json and returns the manifest whose counts and
// per-file hashes describe everything streamed.
func (h *ExportBundleHandler) Handle(ctx context.Context, principal ports.Principal, out io.Writer) (domain.Manifest, error) {
	instanceID, err := h.Instance.InstanceID(ctx)
	if err != nil {
		return domain.Manifest{}, err
	}

	archive := h.Archive(out)
	manifest, err := h.writeBundle(ctx, principal, archive, instanceID)
	closeErr := archive.Close()
	if err != nil {
		return domain.Manifest{}, err
	}
	if closeErr != nil {
		return domain.Manifest{}, closeErr
	}
	return manifest, nil
}

// writeBundle streams every entry and the manifest; any error aborts before
// the manifest is written.
func (h *ExportBundleHandler) writeBundle(ctx context.Context, principal ports.Principal, archive ports.ArchiveWriter, instanceID string) (domain.Manifest, error) {
	var counts domain.ManifestCounts

	todosCount := 0
	err := archive.WriteEntry(ctx, dto.TodosEntry, func(ctx context.Context, w io.Writer) error {
		count, err := h.encodeTodosJSON(ctx, principal, w)
		todosCount = count
		return err
	})
	if err != nil {
		return domain.Manifest{}, err
	}
	counts.Todos = todosCount

	deliveriesCount := 0
	err = archive.WriteEntry(ctx, dto.DeliveriesEntry, func(ctx context.Context, w io.Writer) error {
		count, err := h.encodeDeliveriesJSON(ctx, principal.WorkspaceID, w)
		deliveriesCount = count
		return err
	})
	if err != nil {
		return domain.Manifest{}, err
	}
	counts.Deliveries = deliveriesCount

	channels, err := h.Channels.ExportChannels(ctx, principal)
	if err != nil {
		return domain.Manifest{}, err
	}
	if err := archive.WriteEntry(ctx, dto.PreferencesEntry, func(_ context.Context, w io.Writer) error {
		return encodeJSONArray(w, channels)
	}); err != nil {
		return domain.Manifest{}, err
	}
	counts.Channels = len(channels)

	if err := archive.WriteEntry(ctx, dto.TodosCSVEntry, func(ctx context.Context, w io.Writer) error {
		return h.encodeTodosCSV(ctx, principal, w)
	}); err != nil {
		return domain.Manifest{}, err
	}

	// Files starts non-nil so the archive's digest fill is visible through
	// the returned manifest (the map is shared by reference).
	manifest := domain.Manifest{
		SchemaVersion:    domain.SchemaVersion,
		SourceInstanceID: instanceID,
		ExportedAt:       h.Now(),
		Counts:           counts,
		Files:            map[string]string{},
	}
	if err := archive.WriteManifest(ctx, manifest); err != nil {
		return domain.Manifest{}, err
	}
	return manifest, nil
}

// encodeTodosJSON streams the owner's full-history todos as one JSON array,
// paging through the exporter until a short page.
func (h *ExportBundleHandler) encodeTodosJSON(ctx context.Context, principal ports.Principal, w io.Writer) (int, error) {
	return encodePagedJSON(ctx, w, h.pageSize(), func(ctx context.Context, offset, limit int) ([]dto.TodoExportRecord, error) {
		return h.Todos.ExportTodos(ctx, principal.WorkspaceID, principal.UserID, offset, limit)
	})
}

// encodeDeliveriesJSON streams the workspace's delivery history as one JSON
// array, paging through the exporter until a short page.
func (h *ExportBundleHandler) encodeDeliveriesJSON(ctx context.Context, workspaceID string, w io.Writer) (int, error) {
	return encodePagedJSON(ctx, w, h.pageSize(), func(ctx context.Context, offset, limit int) ([]dto.DeliveryExportRecord, error) {
		return h.Deliveries.ExportDeliveries(ctx, workspaceID, offset, limit)
	})
}

// encodePagedJSON streams a paged record source as one JSON array, counting
// the records it encodes. An empty source encodes as [] rather than null.
func encodePagedJSON[T any](ctx context.Context, w io.Writer, pageSize int, fetch func(context.Context, int, int) ([]T, error)) (int, error) {
	count := 0
	if _, err := io.WriteString(w, "["); err != nil {
		return count, err
	}
	for offset := 0; ; {
		records, err := fetch(ctx, offset, pageSize)
		if err != nil {
			return count, err
		}
		for _, record := range records {
			if count > 0 {
				if _, err := io.WriteString(w, ","); err != nil {
					return count, err
				}
			}
			data, err := json.Marshal(record)
			if err != nil {
				return count, err
			}
			if _, err := w.Write(data); err != nil {
				return count, err
			}
			count++
		}
		if len(records) < pageSize {
			break
		}
		offset += len(records)
	}
	if _, err := io.WriteString(w, "]"); err != nil {
		return count, err
	}
	return count, nil
}

// encodeJSONArray encodes a single record slice as one JSON array; an empty
// slice encodes as [] rather than null.
func encodeJSONArray(w io.Writer, records []dto.ChannelExportRecord) error {
	if _, err := io.WriteString(w, "["); err != nil {
		return err
	}
	for index, record := range records {
		if index > 0 {
			if _, err := io.WriteString(w, ","); err != nil {
				return err
			}
		}
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "]")
	return err
}

// encodeTodosCSV re-pages the same todo source as todos.json and writes the
// human-readable CSV copy: the header row always appears, even for an empty
// workspace.
func (h *ExportBundleHandler) encodeTodosCSV(ctx context.Context, principal ports.Principal, w io.Writer) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(dto.TodosCSVHeader); err != nil {
		return err
	}
	pageSize := h.pageSize()
	for offset := 0; ; {
		records, err := h.Todos.ExportTodos(ctx, principal.WorkspaceID, principal.UserID, offset, pageSize)
		if err != nil {
			return err
		}
		for _, record := range records {
			if err := writer.Write(todoCSVRow(record)); err != nil {
				return err
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return err
		}
		if len(records) < pageSize {
			break
		}
		offset += len(records)
	}
	return writer.Error()
}

// todoCSVRow renders one record in TodosCSVHeader column order; absent
// optional fields stay empty. Times match the JSON encoding so the CSV copy
// reads exactly like todos.json.
func todoCSVRow(record dto.TodoExportRecord) []string {
	return []string{
		record.ID,
		record.Title,
		record.Status,
		csvTime(record.DueAtUTC),
		csvString(record.TimezoneAtInput),
		record.CreatedAt.Format(time.RFC3339Nano),
		csvTime(record.CompletedAt),
		csvTime(record.DeletedAt),
	}
}

func csvTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func csvString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (h *ExportBundleHandler) pageSize() int {
	if h.PageSize <= 0 {
		return defaultPageSize
	}
	return h.PageSize
}
