package postgres

import (
	"context"
	"errors"
	"strconv"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HistoryStore struct{ pool *pgxpool.Pool }

func NewHistoryStore(pool *pgxpool.Pool) *HistoryStore { return &HistoryStore{pool: pool} }

var _ ports.HistoryExporter = (*HistoryStore)(nil)
var _ ports.HistoryImporter = (*HistoryStore)(nil)

func (s *HistoryStore) ExportSessions(ctx context.Context, workspace, user string, offset, limit int) ([]dto.HistorySession, error) {
	rows, err := database.ExecutorFromContextOr(ctx, s.pool).Query(ctx, `select id,title,created_at,updated_at from conversation.sessions where workspace_id=$1 and user_id=$2 order by created_at,id limit $3 offset $4`, workspace, user, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []dto.HistorySession{}
	for rows.Next() {
		var r dto.HistorySession
		if err := rows.Scan(&r.ID, &r.Title, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *HistoryStore) ExportMessages(ctx context.Context, workspace, user string, offset, limit int) ([]dto.HistoryMessage, error) {
	rows, err := database.ExecutorFromContextOr(ctx, s.pool).Query(ctx, `select id,session_id,role,body,resolved_intent,created_at from conversation.messages where workspace_id=$1 and user_id=$2 order by id limit $3 offset $4`, workspace, user, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []dto.HistoryMessage{}
	for rows.Next() {
		var r dto.HistoryMessage
		var id int64
		if err := rows.Scan(&id, &r.SessionID, &r.Role, &r.Body, &r.ResolvedIntent, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.ID = strconv.FormatInt(id, 10)
		r.Order = id
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *HistoryStore) ImportSession(ctx context.Context, workspace, user, id string, r dto.HistorySession) error {
	_, err := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `insert into conversation.sessions(id,workspace_id,user_id,title,created_at,updated_at) values($1,$2,$3,$4,$5,$6)`, id, workspace, user, r.Title, r.CreatedAt, r.UpdatedAt)
	return err
}

func (s *HistoryStore) ImportMessage(ctx context.Context, workspace, user string, r dto.HistoryMessage) (string, error) {
	var id int64
	// The insert itself checks the parent owner; the ambient import transaction
	// preserves ordering and prevents a foreign session FK from being accepted.
	err := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `insert into conversation.messages(workspace_id,user_id,session_id,role,body,resolved_intent,created_at)
 select $1,$2,$3,$4,$5,$6,$7 where $3::uuid is null or exists(select 1 from conversation.sessions where id=$3 and workspace_id=$1 and user_id=$2)
 returning id`, workspace, user, r.SessionID, r.Role, r.Body, r.ResolvedIntent, r.CreatedAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrSessionNotFound
	}
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(id, 10), nil
}
