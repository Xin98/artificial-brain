package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"strings"
)

type MutationExecutor struct {
	UOW      ports.UnitOfWork
	Requests ports.RequestStore
}
type rejection struct {
	ErrorCode string `json:"errorCode"`
}

func BusinessError(code string) error {
	for _, e := range []error{domain.ErrInvalidInput, domain.ErrNotFound, domain.ErrOverflow, domain.ErrDataNotConfigured, domain.ErrDataStale, domain.ErrInsufficientUniverse, domain.ErrFactorUnavailable, domain.ErrCorporateActionIncomplete, domain.ErrInsufficientCash, domain.ErrRiskLimit, domain.ErrAutomationPaused, domain.ErrVersionConflict, domain.ErrIdempotencyConflict, domain.ErrOrderNotCancellable} {
		if e.Error() == code {
			return e
		}
	}
	return nil
}
func (m MutationExecutor) Execute(ctx context.Context, mutation dto.Mutation, input any, work func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	if mutation.Scope.WorkspaceID == "" || mutation.Scope.OwnerUserID == "" || len(mutation.Key) < 1 || len(mutation.Key) > 200 || strings.TrimSpace(mutation.Key) != mutation.Key || mutation.Route == "" {
		return nil, domain.ErrInvalidInput
	}
	b, e := json.Marshal(input)
	if e != nil {
		return nil, domain.ErrInvalidInput
	}
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	var result json.RawMessage
	e = m.UOW.Run(ctx, func(ctx context.Context) error {
		record, e := m.Requests.Claim(ctx, mutation.Scope, mutation.Route, mutation.Key, hash)
		if e != nil {
			return e
		}
		if record.Hash != hash {
			return domain.ErrIdempotencyConflict
		}
		if len(record.Response) > 0 {
			result = record.Response
			return nil
		}
		result, e = work(ctx)
		if e != nil {
			return e
		}
		return m.Requests.Complete(ctx, mutation.Scope, mutation.Route, mutation.Key, result)
	})
	if e != nil {
		return nil, e
	}
	var rejected rejection
	if json.Unmarshal(result, &rejected) == nil && rejected.ErrorCode != "" {
		if e := BusinessError(rejected.ErrorCode); e != nil {
			return nil, e
		}
	}
	return result, nil
}

// Handlers validate business rules before making changes. Business refusals are
// committed as closed responses; infrastructure errors roll back all writes.
func RunMutation[T any](m MutationExecutor, ctx context.Context, mutation dto.Mutation, input any, work func(context.Context) (T, error)) (T, error) {
	var zero T
	b, e := m.Execute(ctx, mutation, input, func(ctx context.Context) (json.RawMessage, error) {
		v, e := work(ctx)
		if e != nil {
			if known := BusinessError(e.Error()); known != nil && errors.Is(e, known) {
				b, er := json.Marshal(rejection{e.Error()})
				return b, er
			}
			return nil, e
		}
		b, e := json.Marshal(v)
		return b, e
	})
	if e != nil {
		return zero, e
	}
	e = json.Unmarshal(b, &zero)
	return zero, e
}
