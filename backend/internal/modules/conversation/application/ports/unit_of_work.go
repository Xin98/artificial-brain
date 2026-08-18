package ports

import "context"

// UnitOfWork runs a unit of work atomically. The cmd composition wires a
// joinable runner so dispatched Todo writes and the conversation audit row
// commit or roll back as one transaction.
type UnitOfWork interface {
	Run(ctx context.Context, work func(context.Context) error) error
}
