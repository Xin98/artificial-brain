package http

import (
	"fmt"
	"io"
	"net/http"
)

// export streams the caller's full-history export bundle as a zip download.
//
// The Content-Type and Content-Disposition headers are set BEFORE the
// handler is invoked: the handler streams the archive straight into the
// response writer, so the first byte it writes flushes the 200 status line.
// A failure before any byte was written still switches to the mapped JSON
// error envelope; once streaming has started the response is committed and a
// mid-stream failure can no longer switch to JSON (standard streaming
// caveat) — the partial body is left as-is.
func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	filename := fmt.Sprintf("artificial-brain-export-%s.zip", h.Export.Now().UTC().Format("20060102"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))

	streamed := &countingWriter{writer: w}
	if _, err := h.Export.Handle(r.Context(), principal, streamed); err != nil {
		if streamed.written > 0 {
			return
		}
		w.Header().Del("Content-Disposition")
		writePortabilityError(w, r, err)
		return
	}
}

// countingWriter counts the bytes written through it so export can tell
// whether streaming has already started when the handler reports an error.
type countingWriter struct {
	writer  io.Writer
	written int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.writer.Write(p)
	c.written += int64(n)
	return n, err
}
