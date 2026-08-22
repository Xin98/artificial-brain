// Package archive implements the Portability module's archive seam: a
// streaming zip Writer that satisfies ports.ArchiveWriter on the export path,
// and a Parse function plus Parser that satisfy ports.BundleParser on the
// import path. It is stdlib only — no database, no other context — and never
// buffers the bundle: entries stream straight into the caller's writer.
package archive

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

// Writer streams bundle entries into a zip archive. It records the sha256 hex
// digest of every entry it writes so WriteManifest can fill the manifest's
// shared Files map before appending manifest.json as the final entry.
type Writer struct {
	zip    *zip.Writer
	hashes map[string]string
}

// NewWriter returns a Writer that streams into w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{
		zip:    zip.NewWriter(w),
		hashes: map[string]string{},
	}
}

// Factory returns the ports.ArchiveFactory cmd/api injects into the export
// handler: each call builds a fresh Writer streaming into w.
func Factory() ports.ArchiveFactory {
	return func(w io.Writer) ports.ArchiveWriter { return NewWriter(w) }
}

var _ ports.ArchiveWriter = (*Writer)(nil)

// WriteEntry opens name in the zip and tees encode's output through sha256,
// recording the hex digest under the entry name. An encode failure aborts the
// entry and surfaces unchanged; nothing is hashed for a failed entry.
func (w *Writer) WriteEntry(ctx context.Context, name string, encode func(context.Context, io.Writer) error) error {
	entry, err := w.zip.Create(name)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	if err := encode(ctx, io.MultiWriter(entry, hasher)); err != nil {
		return err
	}
	w.hashes[name] = hex.EncodeToString(hasher.Sum(nil))
	return nil
}

// WriteManifest fills the manifest's Files map — shared by reference, so the
// caller observes the digests — from the recorded hashes, then marshals the
// wire shape and appends manifest.json as the final entry. The map must be
// non-nil: filling a nil map could never be observed by the caller.
func (w *Writer) WriteManifest(_ context.Context, manifest domain.Manifest) error {
	if manifest.Files == nil {
		return errors.New("archive: manifest files map must be non-nil: the writer fills the shared map")
	}
	for name, checksum := range w.hashes {
		manifest.Files[name] = checksum
	}
	wire, err := json.Marshal(dto.NewBundleManifest(manifest))
	if err != nil {
		return err
	}
	entry, err := w.zip.Create(dto.ManifestEntry)
	if err != nil {
		return err
	}
	_, err = entry.Write(wire)
	return err
}

// Close finalizes the zip archive; an unfinalized archive is a failed export.
func (w *Writer) Close() error {
	return w.zip.Close()
}
