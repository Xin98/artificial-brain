package config

import (
	"fmt"
	"time"
)

const (
	RoleAPI     Role = "api"
	RoleWorker  Role = "worker"
	RoleMigrate Role = "migrate"
)

const (
	defaultAPIAddress         = ":8080"
	defaultWorkerAddress      = ":8081"
	defaultMigrationsDir      = "/migrations"
	defaultServiceVersion     = "dev"
	defaultShutdownTimeout    = 10 * time.Second
	defaultHeartbeatInterval  = 2 * time.Second
	defaultWorkerLeaseTTL     = 6 * time.Second
	minimumLeaseTTLMultiplier = 2
)

type Role string

type LookupEnv func(string) (string, bool)

type Config struct {
	Role              Role
	ServiceName       string
	ServiceVersion    string
	DatabaseURL       string
	HTTPAddress       string
	MigrationsDir     string
	ShutdownTimeout   time.Duration
	HeartbeatInterval time.Duration
	WorkerLeaseTTL    time.Duration
}

func Load(role Role, lookup LookupEnv) (Config, error) {
	serviceName, address, err := roleDefaults(role)
	if err != nil {
		return Config{}, err
	}

	databaseURL, ok := lookup("DATABASE_URL")
	if !ok || databaseURL == "" {
		return Config{}, fmt.Errorf("config: missing DATABASE_URL")
	}

	shutdownTimeout, err := duration(lookup, "SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}
	heartbeatInterval, err := duration(lookup, "WORKER_HEARTBEAT_INTERVAL", defaultHeartbeatInterval)
	if err != nil {
		return Config{}, err
	}
	workerLeaseTTL, err := duration(lookup, "WORKER_LEASE_TTL", defaultWorkerLeaseTTL)
	if err != nil {
		return Config{}, err
	}
	if workerLeaseTTL < minimumLeaseTTLMultiplier*heartbeatInterval {
		return Config{}, fmt.Errorf("config: invalid WORKER_LEASE_TTL")
	}

	return Config{
		Role:              role,
		ServiceName:       serviceName,
		ServiceVersion:    valueOrDefault(lookup, "SERVICE_VERSION", defaultServiceVersion),
		DatabaseURL:       databaseURL,
		HTTPAddress:       valueOrDefault(lookup, addressEnvironmentKey(role), address),
		MigrationsDir:     valueOrDefault(lookup, "MIGRATIONS_DIR", defaultMigrationsDir),
		ShutdownTimeout:   shutdownTimeout,
		HeartbeatInterval: heartbeatInterval,
		WorkerLeaseTTL:    workerLeaseTTL,
	}, nil
}

func addressEnvironmentKey(role Role) string {
	switch role {
	case RoleAPI:
		return "API_HTTP_ADDRESS"
	case RoleWorker:
		return "WORKER_HEALTH_ADDRESS"
	default:
		return ""
	}
}

func roleDefaults(role Role) (string, string, error) {
	switch role {
	case RoleAPI:
		return "api", defaultAPIAddress, nil
	case RoleWorker:
		return "worker", defaultWorkerAddress, nil
	case RoleMigrate:
		return "migrate", "", nil
	default:
		return "", "", fmt.Errorf("config: invalid role")
	}
}

func duration(lookup LookupEnv, key string, fallback time.Duration) (time.Duration, error) {
	value, ok := lookup(key)
	if !ok || value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("config: invalid %s", key)
	}
	return parsed, nil
}

func valueOrDefault(lookup LookupEnv, key, fallback string) string {
	if key == "" {
		return fallback
	}
	value, ok := lookup(key)
	if !ok || value == "" {
		return fallback
	}
	return value
}
