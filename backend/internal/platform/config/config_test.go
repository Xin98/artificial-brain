package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadValidRoles(t *testing.T) {
	tests := []struct {
		name        string
		role        Role
		serviceName string
		address     string
	}{
		{name: "api", role: RoleAPI, serviceName: "api", address: ":8080"},
		{name: "worker", role: RoleWorker, serviceName: "worker", address: ":8081"},
		{name: "migrate", role: RoleMigrate, serviceName: "migrate", address: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(tt.role, mapLookup(map[string]string{
				"DATABASE_URL":    "postgres://user:secret@db/workbench",
				"SERVICE_VERSION": "abc123",
			}))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Role != tt.role {
				t.Fatalf("role = %q, want %q", cfg.Role, tt.role)
			}
			if cfg.ServiceName != tt.serviceName {
				t.Fatalf("service name = %q, want %q", cfg.ServiceName, tt.serviceName)
			}
			if cfg.HTTPAddress != tt.address {
				t.Fatalf("address = %q, want %q", cfg.HTTPAddress, tt.address)
			}
			if cfg.ServiceVersion != "abc123" {
				t.Fatalf("version = %q", cfg.ServiceVersion)
			}
			if cfg.ShutdownTimeout != 10*time.Second {
				t.Fatalf("shutdown timeout = %s", cfg.ShutdownTimeout)
			}
			if cfg.HeartbeatInterval != 2*time.Second {
				t.Fatalf("heartbeat interval = %s", cfg.HeartbeatInterval)
			}
			if cfg.WorkerLeaseTTL != 6*time.Second {
				t.Fatalf("worker lease ttl = %s", cfg.WorkerLeaseTTL)
			}
			if cfg.MigrationsDir != "/migrations" {
				t.Fatalf("migrations directory = %q", cfg.MigrationsDir)
			}
		})
	}
}

func TestLoadWorkerConfig(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":              "postgres://user:secret@db/workbench",
		"SERVICE_VERSION":           "abc123",
		"WORKER_HEARTBEAT_INTERVAL": "3s",
		"WORKER_LEASE_TTL":          "9s",
	}
	cfg, err := Load(RoleWorker, mapLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddress != ":8081" {
		t.Fatalf("address = %q", cfg.HTTPAddress)
	}
	if cfg.HeartbeatInterval != 3*time.Second {
		t.Fatalf("interval = %s", cfg.HeartbeatInterval)
	}
	if cfg.WorkerLeaseTTL != 9*time.Second {
		t.Fatalf("ttl = %s", cfg.WorkerLeaseTTL)
	}
}

func TestLoadUsesRoleSpecificHTTPAddressOverrides(t *testing.T) {
	tests := []struct {
		name, address string
		role          Role
		env           map[string]string
	}{
		{"api", "127.0.0.1:9080", RoleAPI, map[string]string{"DATABASE_URL": "postgres://db/app", "API_HTTP_ADDRESS": "127.0.0.1:9080", "WORKER_HEALTH_ADDRESS": "127.0.0.1:9081"}},
		{"worker", "127.0.0.1:9081", RoleWorker, map[string]string{"DATABASE_URL": "postgres://db/app", "API_HTTP_ADDRESS": "127.0.0.1:9080", "WORKER_HEALTH_ADDRESS": "127.0.0.1:9081"}},
		{"migrate ignores addresses", "", RoleMigrate, map[string]string{"DATABASE_URL": "postgres://db/app", "API_HTTP_ADDRESS": "127.0.0.1:9080", "WORKER_HEALTH_ADDRESS": "127.0.0.1:9081"}},
		{"empty worker retains default", ":8081", RoleWorker, map[string]string{"DATABASE_URL": "postgres://db/app", "WORKER_HEALTH_ADDRESS": ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(tt.role, mapLookup(tt.env))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HTTPAddress != tt.address {
				t.Fatalf("HTTPAddress = %q, want %q", cfg.HTTPAddress, tt.address)
			}
		})
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		role Role
		env  map[string]string
		key  string
	}{
		{name: "missing database URL", role: RoleAPI, env: map[string]string{}, key: "DATABASE_URL"},
		{name: "invalid shutdown timeout", role: RoleAPI, env: map[string]string{"DATABASE_URL": "postgres://user:secret@db/workbench", "SHUTDOWN_TIMEOUT": "invalid"}, key: "SHUTDOWN_TIMEOUT"},
		{name: "invalid heartbeat interval", role: RoleWorker, env: map[string]string{"DATABASE_URL": "postgres://user:secret@db/workbench", "WORKER_HEARTBEAT_INTERVAL": "invalid"}, key: "WORKER_HEARTBEAT_INTERVAL"},
		{name: "invalid worker lease ttl", role: RoleWorker, env: map[string]string{"DATABASE_URL": "postgres://user:secret@db/workbench", "WORKER_LEASE_TTL": "invalid"}, key: "WORKER_LEASE_TTL"},
		{name: "lease shorter than two heartbeats", role: RoleWorker, env: map[string]string{"DATABASE_URL": "postgres://user:secret@db/workbench", "WORKER_HEARTBEAT_INTERVAL": "3s", "WORKER_LEASE_TTL": "5s"}, key: "WORKER_LEASE_TTL"},
		{name: "unknown role", role: Role("unknown"), env: map[string]string{"DATABASE_URL": "postgres://user:secret@db/workbench"}, key: "role"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(tt.role, mapLookup(tt.env))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("error = %q, want key %q", err, tt.key)
			}
		})
	}
}

func TestLoadNeverEchoesSecretValue(t *testing.T) {
	_, err := Load(RoleAPI, mapLookup(map[string]string{
		"DATABASE_URL":     "postgres://user:TOP-SECRET@db/workbench",
		"SHUTDOWN_TIMEOUT": "not-a-duration",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "TOP-SECRET") {
		t.Fatalf("secret leaked: %v", err)
	}
}

func mapLookup(values map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
