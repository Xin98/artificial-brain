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
// unified model turn, strict envelope validation, clarification gate, then
// router dispatch. Every turn persists a user row plus a paired assistant
// row inside the same unit of work as any write, against an explicit or
// auto-created session.
type ProcessMessageHandler struct {
	Model             ports.ModelPort
	Todos             ports.TodoGateway
	Confirmations     ports.ConfirmationStore
	Sessions          ports.SessionStore
	Messages          ports.MessageLogStore
	UoW               ports.UnitOfWork
	Router            *application.Router
	NewConfirmationID func() string
	NewSessionID      func() string
	Now               func() time.Time
	ConfirmationTTL   time.Duration
	// HistoryTurns bounds the transcript rows handed to the model as
	// multi-turn context; 0 disables history entirely.
	HistoryTurns int
}

// Handle processes the user turn for the caller's workspace. sessionID may
// be empty: the session is then auto-created inside the turn's unit of work
// with a title derived from the message. An explicit sessionID is ownership-
// checked first; a miss yields domain.ErrSessionNotFound.
func (h *ProcessMessageHandler) Handle(ctx context.Context, workspaceID, userID, sessionID, text, timezone string) (dto.MessageResponse, error) {
	if sessionID != "" {
		if _, err := h.Sessions.Get(ctx, workspaceID, userID, sessionID); err != nil {
			return dto.MessageResponse{}, err
		}
	}

	history, err := h.loadHistory(ctx, workspaceID, userID, sessionID)
	if err != nil {
		return dto.MessageResponse{}, err
	}

	raw, err := h.Model.Complete(ctx, ports.MessageInput{Text: text, Timezone: timezone, History: history})
	if err != nil {
		return dto.MessageResponse{}, err
	}
	turn, err := application.ValidateModelTurn(raw)
	if err != nil {
		// Fail closed: the transcript records the turn as unsupported with
		// fixed copy; nothing dispatches and the reply never surfaces.
		resolved, persistErr := h.persistTranscript(ctx, workspaceID, userID, sessionID, text,
			application.ResolvedIntentUnsupported, application.UnsupportedSummary)
		if persistErr != nil {
			return dto.MessageResponse{}, persistErr
		}
		return dto.MessageResponse{Kind: dto.KindUnsupported, SessionID: resolved}, nil
	}
	if turn.Proposal == nil || !h.Router.Supports(turn.Proposal.Intent) {
		resolved, persistErr := h.persistTranscript(ctx, workspaceID, userID, sessionID, text,
			application.ResolvedIntentChat, turn.Reply)
		if persistErr != nil {
			return dto.MessageResponse{}, persistErr
		}
		return dto.MessageResponse{Kind: dto.KindChat, Reply: turn.Reply, SessionID: resolved}, nil
	}
	proposal := *turn.Proposal
	if len(proposal.MissingFields) > 0 {
		return h.clarify(ctx, workspaceID, userID, sessionID, text, proposal.MissingFields, turn.Reply)
	}
	if proposal.Confidence < application.MinDispatchConfidence {
		return h.clarify(ctx, workspaceID, userID, sessionID, text, nil, turn.Reply)
	}
	switch proposal.Intent {
	case domain.IntentTodoCreate:
		return h.dispatchCreate(ctx, workspaceID, userID, sessionID, text, timezone, turn.Reply, proposal)
	case domain.IntentTodoList:
		return h.dispatchList(ctx, workspaceID, userID, sessionID, text, proposal)
	case domain.IntentTodoDelete:
		return h.dispatchDelete(ctx, workspaceID, userID, sessionID, text, turn.Reply, proposal)
	}
	return dto.MessageResponse{Kind: dto.KindUnsupported}, nil
}

// loadHistory reads the session transcript window for the model. Bodies are
// truncated to application.MaxHistoryBodyRunes; a fresh (auto) session has
// no history yet.
func (h *ProcessMessageHandler) loadHistory(ctx context.Context, workspaceID, userID, sessionID string) ([]ports.HistoryMessage, error) {
	if h.HistoryTurns <= 0 || sessionID == "" {
		return nil, nil
	}
	entries, err := h.Messages.ListBySession(ctx, workspaceID, userID, sessionID, h.HistoryTurns)
	if err != nil {
		return nil, err
	}
	history := make([]ports.HistoryMessage, 0, len(entries))
	for _, entry := range entries {
		history = append(history, ports.HistoryMessage{
			Role: entry.Role,
			Text: application.TruncateHistoryBody(entry.Body),
		})
	}
	return history, nil
}

func (h *ProcessMessageHandler) dispatchCreate(ctx context.Context, workspaceID, userID, sessionID, text, timezone, reply string, proposal domain.IntentProposal) (dto.MessageResponse, error) {
	if proposal.Arguments.Title == "" {
		return h.clarify(ctx, workspaceID, userID, sessionID, text, []string{"title"}, reply)
	}
	echoTimezone := proposal.Arguments.Timezone
	if echoTimezone == "" {
		echoTimezone = timezone
	}
	response := dto.MessageResponse{Kind: dto.KindTodoCreated}
	if proposal.Arguments.DueAtUTC != nil {
		due := *proposal.Arguments.DueAtUTC
		response.ResolvedDueAtUTC = &due
		response.TimezoneEcho = echoTimezone
		if location, err := time.LoadLocation(echoTimezone); err == nil {
			response.LocalEcho = due.In(location).Format("2006-01-02 15:04")
		}
	}
	var created tododto.Todo
	var resolved string
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
		summary := application.CreateSchedulingSummary(todo, response.LocalEcho, echoTimezone, proposal.Arguments.DueAtUTC)
		id, err := h.transcribeInside(ctx, workspaceID, userID, sessionID, text, string(proposal.Intent), summary)
		resolved = id
		return err
	})
	if err != nil {
		return dto.MessageResponse{}, err
	}
	response.Todo = &created
	response.SessionID = resolved
	return response, nil
}

func (h *ProcessMessageHandler) dispatchList(ctx context.Context, workspaceID, userID, sessionID, text string, proposal domain.IntentProposal) (dto.MessageResponse, error) {
	filters := tododto.ListFilters{
		Keyword: proposal.Arguments.Keyword,
		Status:  proposal.Arguments.Status,
		DueFrom: proposal.Arguments.DueFrom,
		DueTo:   proposal.Arguments.DueTo,
		NoDue:   proposal.Arguments.NoDue,
	}
	var todos []tododto.Todo
	var resolved string
	err := h.UoW.Run(ctx, func(ctx context.Context) error {
		listed, err := h.Todos.ListTodos(ctx, workspaceID, userID, filters)
		if err != nil {
			return err
		}
		todos = listed
		id, err := h.transcribeInside(ctx, workspaceID, userID, sessionID, text,
			string(proposal.Intent), application.TodoListSummary(todos))
		resolved = id
		return err
	})
	if err != nil {
		return dto.MessageResponse{}, err
	}
	if todos == nil {
		todos = []tododto.Todo{}
	}
	return dto.MessageResponse{Kind: dto.KindTodoList, Todos: todos, SessionID: resolved}, nil
}

func (h *ProcessMessageHandler) dispatchDelete(ctx context.Context, workspaceID, userID, sessionID, text, reply string, proposal domain.IntentProposal) (dto.MessageResponse, error) {
	candidates, err := h.Todos.SearchCandidates(ctx, workspaceID, userID, proposal.Arguments.Keyword)
	if err != nil {
		return dto.MessageResponse{}, err
	}
	switch {
	case len(candidates) == 0:
		resolved, err := h.persistTranscript(ctx, workspaceID, userID, sessionID, text,
			string(proposal.Intent), application.NotFoundSummary)
		if err != nil {
			return dto.MessageResponse{}, err
		}
		return dto.MessageResponse{Kind: dto.KindNotFound, SessionID: resolved}, nil
	case len(candidates) > maxSelectableCandidates:
		return h.clarify(ctx, workspaceID, userID, sessionID, text, nil, reply)
	case len(candidates) == 1:
		confirmation, err := domain.NewConfirmationRequest(h.NewConfirmationID(), workspaceID, userID,
			domain.IntentTodoDelete, candidates[0].TodoID, candidates[0].Version, h.Now(), h.ConfirmationTTL)
		if err != nil {
			return dto.MessageResponse{}, err
		}
		var resolved string
		err = h.UoW.Run(ctx, func(ctx context.Context) error {
			if err := h.Confirmations.Save(ctx, confirmation); err != nil {
				return err
			}
			id, err := h.transcribeInside(ctx, workspaceID, userID, sessionID, text,
				string(proposal.Intent), application.ConfirmationSummary(candidates[0].Title))
			resolved = id
			return err
		})
		if err != nil {
			return dto.MessageResponse{}, err
		}
		expiresAt := confirmation.ExpiresAt
		return dto.MessageResponse{
			Kind:           dto.KindConfirmationRequired,
			Candidates:     candidates,
			ConfirmationID: confirmation.ID,
			ExpiresAt:      &expiresAt,
			SessionID:      resolved,
		}, nil
	default:
		resolved, err := h.persistTranscript(ctx, workspaceID, userID, sessionID, text,
			string(proposal.Intent), application.CandidatesSummary(len(candidates)))
		if err != nil {
			return dto.MessageResponse{}, err
		}
		return dto.MessageResponse{Kind: dto.KindCandidates, Candidates: candidates, SessionID: resolved}, nil
	}
}

// clarify persists the turn as a clarification and echoes the model's reply
// when it supplied one (a defensive clarification inside dispatch may pass
// an empty reply).
func (h *ProcessMessageHandler) clarify(ctx context.Context, workspaceID, userID, sessionID, text string, missingFields []string, reply string) (dto.MessageResponse, error) {
	assistantBody := reply
	if assistantBody == "" {
		assistantBody = application.UnsupportedSummary
	}
	resolved, err := h.persistTranscript(ctx, workspaceID, userID, sessionID, text,
		application.ResolvedIntentClarification, assistantBody)
	if err != nil {
		return dto.MessageResponse{}, err
	}
	return dto.MessageResponse{
		Kind:          dto.KindClarification,
		MissingFields: missingFields,
		Reply:         reply,
		SessionID:     resolved,
	}, nil
}

// persistTranscript resolves the session, writes both transcript rows and
// touches the session inside one unit of work; it returns the session id the
// rows were persisted against.
func (h *ProcessMessageHandler) persistTranscript(ctx context.Context, workspaceID, userID, sessionID, text, resolvedIntent, assistantBody string) (string, error) {
	var resolved string
	err := h.UoW.Run(ctx, func(ctx context.Context) error {
		id, err := h.transcribeInside(ctx, workspaceID, userID, sessionID, text, resolvedIntent, assistantBody)
		resolved = id
		return err
	})
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// transcribeInside writes the user row, the paired assistant row and the
// session touch without opening a unit of work; callers run it inside one.
// An empty sessionID auto-creates the session from the first message.
func (h *ProcessMessageHandler) transcribeInside(ctx context.Context, workspaceID, userID, sessionID, text, resolvedIntent, assistantBody string) (string, error) {
	resolved := sessionID
	if resolved == "" {
		id := h.NewSessionID()
		session, err := domain.NewSession(id, workspaceID, userID, domain.DefaultSessionTitle(text), h.Now())
		if err != nil {
			return "", err
		}
		if err := h.Sessions.Create(ctx, session); err != nil {
			return "", err
		}
		resolved = id
	}
	now := h.Now()
	intent := resolvedIntent
	scope := resolved
	if err := h.Messages.Append(ctx, ports.MessageLog{
		WorkspaceID:    workspaceID,
		UserID:         userID,
		Role:           ports.RoleUser,
		Body:           text,
		SessionID:      &scope,
		ResolvedIntent: &intent,
		CreatedAt:      now,
	}); err != nil {
		return "", err
	}
	if err := h.Messages.Append(ctx, ports.MessageLog{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Role:        ports.RoleAssistant,
		Body:        assistantBody,
		SessionID:   &scope,
		CreatedAt:   now,
	}); err != nil {
		return "", err
	}
	if err := h.Sessions.Touch(ctx, workspaceID, userID, resolved, now); err != nil {
		return "", err
	}
	return resolved, nil
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
