package archive

import (
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

func checkVersionedEntrySet(entries map[string][]byte, version string) error {
	names := append([]string{}, requiredEntries...)
	if version == "2" {
		names = append(names, dto.SessionsEntry, dto.MessagesEntry)
	}
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
		if _, ok := entries[name]; !ok {
			return fmt.Errorf("%w: missing entry %q", domain.ErrBundleStructure, name)
		}
	}
	for name := range entries {
		if !allowed[name] {
			return fmt.Errorf("%w: unexpected entry %q", domain.ErrBundleStructure, name)
		}
	}
	return nil
}
