package policy_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Xin98/artificial-brain/architecture/policy"
)

func TestValidateFixtures(t *testing.T) {
	tests := []struct {
		name string
		root string
		want []policy.Violation
	}{
		{
			name: "valid",
			root: "testdata/valid",
		},
		{
			name: "domain imports adapter",
			root: "testdata/invalid-domain",
			want: []policy.Violation{{
				File:   "backend/internal/modules/todo/domain/todo.go",
				Rule:   "go-domain-dependency",
				Import: "github.com/Xin98/artificial-brain/backend/internal/modules/todo/adapters/postgres",
			}},
		},
		{
			name: "domain rejects a prefix that is not its domain package",
			root: "testdata/invalid-domain-prefix",
			want: []policy.Violation{{
				File:   "backend/internal/modules/todo/domain/todo.go",
				Rule:   "go-domain-dependency",
				Import: "github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain_evil",
			}},
		},
		{
			name: "application imports adapter",
			root: "testdata/invalid-application",
			want: []policy.Violation{{
				File:   "backend/internal/modules/todo/application/create.go",
				Rule:   "go-application-dependency",
				Import: "github.com/Xin98/artificial-brain/backend/internal/modules/todo/adapters/postgres",
			}},
		},
		{
			name: "cross context internal import",
			root: "testdata/invalid-cross-context",
			want: []policy.Violation{{
				File:   "backend/internal/modules/todo/application/create.go",
				Rule:   "go-cross-context",
				Import: "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain",
			}},
		},
		{
			name: "portability imports another context's domain",
			root: "testdata/invalid-cross-context-portability",
			want: []policy.Violation{{
				File:   "backend/internal/modules/portability/application/bad.go",
				Rule:   "go-cross-context",
				Import: "github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain",
			}},
		},
		{
			name: "platform imports business",
			root: "testdata/invalid-platform",
			want: []policy.Violation{{
				File:   "backend/internal/platform/database/bad.go",
				Rule:   "go-platform-dependency",
				Import: "github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain",
			}},
		},
		{
			name: "web feature reads deployment env",
			root: "testdata/invalid-web",
			want: []policy.Violation{{
				File:   "apps/web/src/features/system-health/bad.ts",
				Rule:   "web-feature-env",
				Import: "process.env.API_INTERNAL_URL",
			}},
		},
		{
			name: "web feature reads Compose hosts",
			root: "testdata/invalid-web-compose",
			want: []policy.Violation{
				{File: "apps/web/src/features/system-health/bad.ts", Rule: "web-feature-env", Import: "api"},
				{File: "apps/web/src/features/system-health/bad.ts", Rule: "web-feature-env", Import: "api:${port}"},
				{File: "apps/web/src/features/system-health/bad.ts", Rule: "web-feature-env", Import: "api:8080"},
				{File: "apps/web/src/features/system-health/bad.ts", Rule: "web-feature-env", Import: "http://api:${port}"},
				{File: "apps/web/src/features/system-health/bad.ts", Rule: "web-feature-env", Import: "https://api/"},
				{File: "apps/web/src/features/system-health/bad.ts", Rule: "web-feature-env", Import: "postgres://db:${databasePort}"},
				{File: "apps/web/src/features/system-health/bad.ts", Rule: "web-feature-env", Import: "postgres://user@db:5432"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations, err := policy.Validate(filepath.Join("..", "tests", "testdata", filepath.Base(tt.root)))
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.want == nil {
				if len(violations) != 0 {
					t.Fatalf("Validate() violations = %#v, want none", violations)
				}
				return
			}
			if !reflect.DeepEqual(violations, tt.want) {
				t.Fatalf("Validate() violations = %#v, want %#v", violations, tt.want)
			}
		})
	}
}
