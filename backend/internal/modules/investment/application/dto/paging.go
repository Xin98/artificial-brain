package dto

import (
	"encoding/base64"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"strconv"
)

type Page struct {
	After string
	Limit int
}

func ParsePage(cursor, limit string) (Page, error) {
	p := Page{Limit: 25}
	if limit != "" {
		v, e := strconv.Atoi(limit)
		if e != nil || v < 1 || v > 100 {
			return p, domain.ErrInvalidInput
		}
		p.Limit = v
	}
	if cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || len(b) > 200 {
			return p, domain.ErrInvalidInput
		}
		p.After = string(b)
	}
	return p, nil
}
func Cursor(id string) string { return base64.RawURLEncoding.EncodeToString([]byte(id)) }

type List[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor"`
}
