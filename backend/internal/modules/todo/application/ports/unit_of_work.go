package ports

import "context"

// UnitOfWork runs a unit of work atomically. The platform TxRunner
// implements it with a database transaction; handlers compose store writes
// and reminder seam calls inside it and never begin transactions themselves.
type UnitOfWork interface {
	Run(ctx context.Context, work func(context.Context) error) error
}
