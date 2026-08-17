package application

import "github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"

func Create(value string) string { return domain.Name(value) }
