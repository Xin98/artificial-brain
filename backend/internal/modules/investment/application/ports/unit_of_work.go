package ports

import "context"

type UnitOfWork interface {
	Run(context.Context, func(context.Context) error) error
}
