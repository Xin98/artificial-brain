// Package command implements the Conversation application commands.
package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
)

// maxSelectableCandidates bounds the delete-candidate list a user can choose
// from; more than this asks the user to refine the keyword (A13 caps the
// search at 11 so the overflow is detectable).
const maxSelectableCandidates = 10

// ProcessMessageHandler turns one user turn into a controlled outcome:
// model proposal, strict validation, clarification gate, then router
// dispatch. Dispatched turns append the audit row inside the same unit of
// work as any write.
type ProcessMessageHandler struct {
	Model             ports.ModelPort
	Todos             ports.TodoGateway
	Confirmations     ports.ConfirmationStore
	Messages          ports.MessageLogStore
	UoW               ports.UnitOfWork
	Router            *application.Router
	NewConfirmationID func() string
	Now               func() time.Time
	ConfirmationTTL   time.Duration
}

// Handle processes the user turn for the caller's workspace.
func (h *ProcessMessageHandler) Handle(ctx context.Context, workspaceID, userID, text, timezone string) (dto.MessageResponse, error) {
	raw, err := h.Model.Propose(ctx, ports.MessageInput{Text: text, Timezone: timezone})
	if err != nil {
		return dto.MessageResponse{}, err
	}
	proposal, err := application.ValidateProposal(raw)
	if err != nil {
		return dto.MessageResponse{Kind: dto.KindUnsupported}, nil
	}
	if !h.Router.Supports(proposal.Intent) {
		return dto.MessageResponse{Kind: dto.KindUnsupported}, nil
	}
	if len(proposal.MissingFields) > 0 {
		return clarification(proposal.MissingFields), nil
	}
	if proposal.Confidence < application.MinDispatchConfidence {
		return clarification(nil), nil
	}
	switch proposal.Intent {
	case domain.IntentTodoCreate:
		return h.dispatchCreate(ctx, workspaceID, userID, text, timezone, proposal)
	case domain.IntentTodoList:
		return h.dispatchList(ctx, workspaceID, userID, text, proposal)
	case domain.IntentTodoDelete:
		return h.dispatchDelete(ctx, workspaceID, userID, text, proposal)
	}
	return dto.MessageResponse{Kind: dto.KindUnsupported}, nil
}

func (h *ProcessMessageHandler) dispatchCreate(ctx context.Context, workspaceID, userID, text, timezone string, proposal domain.IntentProposal) (dto.MessageResponse, error) {
	if proposal.Arguments.Title == "" {
		return clarification([]string{"title"}), nil
	}
	echoTimezone := proposal.Arguments.Timezone
	if echoTimezone == "" {
		echoTimezone = timezone
	}
	var created tododto.Todo
	err := h.UoW.Run(ctx, func(ctx context.Context) error {
		todo, err := h.Todos.CreateTodo(ctx, tododto.CreateTodoRequest{
			WorkspaceID:     workspaceID,
			UserID:          userID,
			Title:           proposal.Arguments.Title,
			Description:     proposal.Arguments.Description,
			DueAtUTC:        proposal.Arguments.DueAtUTC,
			TimezoneAtInput: stringPointer(echoTimezone),
		})
		if err != nil {
			return err
		}
		created = todo
		return h.appendTurn(ctx, workspaceID, userID, text, proposal.Intent)
	})
	if err != nil {
		return dto.MessageResponse{}, err
	}
	response := dto.MessageResponse{Kind: dto.KindTodoCreated, Todo: &created}
	if proposal.Arguments.DueAtUTC != nil {
		due := *proposal.Arguments.DueAtUTC
		response.ResolvedDueAtUTC = &due
		response.TimezoneEcho = echoTimezone
		if location, err := time.LoadLocation(echoTimezone); err == nil {
			response.LocalEcho = due.In(location).Format("2006-01-02 15:04")
		}
	}
	return response, nil
}

func (h *ProcessMessageHandler) dispatchList(ctx context.Context, workspaceID, userID, text string, proposal domain.IntentProposal) (dto.MessageResponse, error) {
	filters := tododto.ListFilters{
		Keyword: proposal.Arguments.Keyword,
		Status:  proposal.Arguments.Status,
		DueFrom: proposal.Arguments.DueFrom,
		DueTo:   proposal.Arguments.DueTo,
		NoDue:   proposal.Arguments.NoDue,
	}
	var todos []tododto.Todo
	err := h.UoW.Run(ctx, func(ctx context.Context) error {
		listed, err := h.Todos.ListTodos(ctx, workspaceID, userID, filters)
		if err != nil {
			return err
		}
		todos = listed
		return h.appendTurn(ctx, workspaceID, userID, text, proposal.Intent)
	})
	if err != nil {
		return dto.MessageResponse{}, err
	}
	if todos == nil {
		todos = []tododto.Todo{}
	}
	return dto.MessageResponse{Kind: dto.KindTodoList, Todos: todos}, nil
}

func (h *ProcessMessageHandler) dispatchDelete(ctx context.Context, workspaceID, userID, text string, proposal domain.IntentProposal) (dto.MessageResponse, error) {
	candidates, err := h.Todos.SearchCandidates(ctx, workspaceID, userID, proposal.Arguments.Keyword)
	if err != nil {
		return dto.MessageResponse{}, err
	}
	switch {
	case len(candidates) == 0:
		if err := h.appendTurnInsideUoW(ctx, workspaceID, userID, text, proposal.Intent); err != nil {
			return dto.MessageResponse{}, err
		}
		return dto.MessageResponse{Kind: dto.KindNotFound}, nil
	case len(candidates) > maxSelectableCandidates:
		return clarification(nil), nil
	case len(candidates) == 1:
		confirmation, err := domain.NewConfirmationRequest(h.NewConfirmationID(), workspaceID, userID,
			domain.IntentTodoDelete, candidates[0].TodoID, candidates[0].Version, h.Now(), h.ConfirmationTTL)
		if err != nil {
			return dto.MessageResponse{}, err
		}
		err = h.UoW.Run(ctx, func(ctx context.Context) error {
			if err := h.Confirmations.Save(ctx, confirmation); err != nil {
				return err
			}
			return h.appendTurn(ctx, workspaceID, userID, text, proposal.Intent)
		})
		if err != nil {
			return dto.MessageResponse{}, err
		}
		expiresAt := confirmation.ExpiresAt
		return dto.MessageResponse{
			Kind:           dto.KindConfirmationRequired,
			ConfirmationID: confirmation.ID,
			ExpiresAt:      &expiresAt,
		}, nil
	default:
		if err := h.appendTurnInsideUoW(ctx, workspaceID, userID, text, proposal.Intent); err != nil {
			return dto.MessageResponse{}, err
		}
		return dto.MessageResponse{Kind: dto.KindCandidates, Candidates: candidates}, nil
	}
}

func (h *ProcessMessageHandler) appendTurnInsideUoW(ctx context.Context, workspaceID, userID, text string, intent domain.Intent) error {
	return h.UoW.Run(ctx, func(ctx context.Context) error {
		return h.appendTurn(ctx, workspaceID, userID, text, intent)
	})
}

func (h *ProcessMessageHandler) appendTurn(ctx context.Context, workspaceID, userID, text string, intent domain.Intent) error {
	resolved := string(intent)
	return h.Messages.Append(ctx, ports.MessageLog{
		WorkspaceID:    workspaceID,
		UserID:         userID,
		Role:           ports.RoleUser,
		Body:           text,
		ResolvedIntent: &resolved,
		CreatedAt:      h.Now(),
	})
}

func clarification(missingFields []string) dto.MessageResponse {
	return dto.MessageResponse{Kind: dto.KindClarification, MissingFields: missingFields}
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
