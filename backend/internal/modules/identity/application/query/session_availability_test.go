package query

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	"testing"
)

type unavailableSessions struct {
	*fakeSessionStore
	err error
}

func (s unavailableSessions) ByTokenHash(context.Context, string) (domain.Session, error) {
	return domain.Session{}, s.err
}
func TestSessionInfrastructureFailureRemainsFailure(t *testing.T) {
	err := errors.New("database unavailable")
	q := SessionQuery{Sessions: unavailableSessions{err: err}, Now: fixedNow}
	if _, e := q.Authenticate(context.Background(), "cookie"); !errors.Is(e, err) {
		t.Fatal("outage misclassified as expired credentials", e)
	}
}
