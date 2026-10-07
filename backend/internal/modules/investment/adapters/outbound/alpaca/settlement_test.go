package alpaca

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettlementManifestRejectsAmbiguousCalendar(t *testing.T) {
	for _, body := range []string{`{"source":"operator","version":"v1","years":[2026],"days":["2026-10-13"],"unknown":true}`, `{"source":"operator","version":"v1","days":["2026-10-13"]}`, `{"source":"operator","version":"v1","years":[2026],"days":["2026-10-13","2026-10-13"]}`, `{"source":"","version":"v1","years":[2026],"days":["2026-10-13"]}`} {
		p := filepath.Join(t.TempDir(), "calendar.json")
		if e := os.WriteFile(p, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := LoadSettlementCalendar(p); e == nil {
			t.Fatal(body)
		}
	}
}
