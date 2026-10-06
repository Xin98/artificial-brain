package main

import (
	"context"
	"errors"

	conversationcommand "github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/command"
	conversationdto "github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	conversationquery "github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/query"
	conversationdomain "github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	portabilitydto "github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	portabilityports "github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
)

type conversationExportShim struct {
	q *conversationquery.ExportHistoryQuery
}

func (s *conversationExportShim) ExportSessions(ctx context.Context, p portabilityports.Principal, offset, limit int) ([]portabilitydto.SessionExportRecord, error) {
	rows, err := s.q.ExportSessions(ctx, p.WorkspaceID, p.UserID, offset, limit)
	if err != nil {
		return nil, err
	}
	out := make([]portabilitydto.SessionExportRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, portabilitydto.SessionExportRecord{ID: r.ID, Title: r.Title, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt})
	}
	return out, nil
}
func (s *conversationExportShim) ExportMessages(ctx context.Context, p portabilityports.Principal, offset, limit int) ([]portabilitydto.MessageExportRecord, error) {
	rows, err := s.q.ExportMessages(ctx, p.WorkspaceID, p.UserID, offset, limit)
	if err != nil {
		return nil, err
	}
	out := make([]portabilitydto.MessageExportRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, portabilitydto.MessageExportRecord{ID: r.ID, SessionID: r.SessionID, Role: r.Role, Body: r.Body, ResolvedIntent: r.ResolvedIntent, Order: r.Order, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

type conversationImportShim struct {
	h *conversationcommand.ImportHistoryHandler
}

func (s *conversationImportShim) ImportSession(ctx context.Context, p portabilityports.Principal, r portabilitydto.SessionImportRequest) (string, error) {
	return s.h.ImportSession(ctx, p.WorkspaceID, p.UserID, conversationdto.HistorySession{ID: r.ID, Title: r.Title, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt})
}
func (s *conversationImportShim) ImportMessage(ctx context.Context, p portabilityports.Principal, r portabilitydto.MessageImportRequest) (string, error) {
	id, err := s.h.ImportMessage(ctx, p.WorkspaceID, p.UserID, conversationdto.HistoryMessage{ID: r.ID, SessionID: r.SessionID, Role: r.Role, Body: r.Body, ResolvedIntent: r.ResolvedIntent, Order: r.Order, CreatedAt: r.CreatedAt})
	if errors.Is(err, conversationdomain.ErrSessionNotFound) {
		return "", portabilityports.ErrConversationSessionNotFound
	}
	return id, err
}

var _ portabilityports.ConversationExporter = (*conversationExportShim)(nil)
var _ portabilityports.ConversationImporter = (*conversationImportShim)(nil)
