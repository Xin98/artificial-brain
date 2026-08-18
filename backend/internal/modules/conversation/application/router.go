package application

import "github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"

// Router is the intent registry. Only registered intents can reach Todo's
// public application surface; every other proposal is unsupported. The
// registry is fixed at construction — no runtime registration.
type Router struct {
	registered map[domain.Intent]bool
}

// NewRouter returns the registry with exactly the ITER-0002 intents.
func NewRouter() *Router {
	return &Router{registered: map[domain.Intent]bool{
		domain.IntentTodoCreate: true,
		domain.IntentTodoDelete: true,
		domain.IntentTodoList:   true,
	}}
}

// Supports reports whether the intent is dispatchable.
func (r *Router) Supports(intent domain.Intent) bool { return r.registered[intent] }
