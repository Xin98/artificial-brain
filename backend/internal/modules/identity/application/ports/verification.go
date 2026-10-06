package ports

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// VerificationUnitOfWork scopes a code rotation and its channel row lock.
type VerificationUnitOfWork interface {
	Run(context.Context, func(context.Context) error) error
}

// ChannelVerificationStore locks a scoped channel for the caller's ambient
// transaction, serializing resends against verification and other resends.
type ChannelVerificationStore interface {
	ByIDForUpdate(context.Context, string, string, string) (domain.ContactChannel, error)
}

// ChannelEnabledStore changes only the enabled flag in one scoped write,
// retaining concurrent verification results and code rotations.
type ChannelEnabledStore interface {
	SetEnabled(context.Context, string, string, string, bool) (domain.ContactChannel, error)
}
