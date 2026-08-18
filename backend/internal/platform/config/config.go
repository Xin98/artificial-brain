package config

import (
	"fmt"
	"strings"
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

	defaultAppEnv            = "production"
	defaultSessionTTL        = 168 * time.Hour
	defaultLoginChallengeTTL = 5 * time.Minute
	defaultChannelCodeTTL    = 10 * time.Minute
	defaultConfirmationTTL   = 5 * time.Minute
	defaultModelAdapter      = "deterministic"
	defaultModelTimeout      = 15 * time.Second

	ModelAdapterDeterministic    = "deterministic"
	ModelAdapterOpenAICompatible = "openai_compatible"
	AppEnvProduction             = "production"
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

	AppEnv            string
	DevInboxEnabled   bool
	SessionTTL        time.Duration
	LoginChallengeTTL time.Duration
	ChannelCodeTTL    time.Duration
	ConfirmationTTL   time.Duration

	ModelAdapter string
	ModelBaseURL string
	ModelAPIKey  string
	ModelName    string
	ModelTimeout time.Duration
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

	appEnv := valueOrDefault(lookup, "APP_ENV", defaultAppEnv)
	devInboxEnabled, err := boolValue(lookup, "DEV_INBOX_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	if devInboxEnabled && appEnv == AppEnvProduction {
		return Config{}, fmt.Errorf("config: DEV_INBOX_ENABLED requires a non-production APP_ENV")
	}

	sessionTTL, err := duration(lookup, "SESSION_TTL", defaultSessionTTL)
	if err != nil {
		return Config{}, err
	}
	loginChallengeTTL, err := duration(lookup, "LOGIN_CHALLENGE_TTL", defaultLoginChallengeTTL)
	if err != nil {
		return Config{}, err
	}
	channelCodeTTL, err := duration(lookup, "CHANNEL_CODE_TTL", defaultChannelCodeTTL)
	if err != nil {
		return Config{}, err
	}
	confirmationTTL, err := duration(lookup, "CONFIRMATION_TTL", defaultConfirmationTTL)
	if err != nil {
		return Config{}, err
	}

	modelAdapter := valueOrDefault(lookup, "MODEL_ADAPTER", defaultModelAdapter)
	modelBaseURL := valueOrDefault(lookup, "MODEL_BASE_URL", "")
	modelAPIKey := valueOrDefault(lookup, "MODEL_API_KEY", "")
	modelName := valueOrDefault(lookup, "MODEL_NAME", "")
	modelTimeout, err := duration(lookup, "MODEL_TIMEOUT", defaultModelTimeout)
	if err != nil {
		return Config{}, err
	}
	switch modelAdapter {
	case ModelAdapterDeterministic:
	case ModelAdapterOpenAICompatible:
		if modelBaseURL == "" {
			return Config{}, fmt.Errorf("config: missing MODEL_BASE_URL")
		}
		if modelName == "" {
			return Config{}, fmt.Errorf("config: missing MODEL_NAME")
		}
		if modelAPIKey == "" {
			return Config{}, fmt.Errorf("config: missing MODEL_API_KEY")
		}
	default:
		return Config{}, fmt.Errorf("config: invalid MODEL_ADAPTER")
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

		AppEnv:            appEnv,
		DevInboxEnabled:   devInboxEnabled,
		SessionTTL:        sessionTTL,
		LoginChallengeTTL: loginChallengeTTL,
		ChannelCodeTTL:    channelCodeTTL,
		ConfirmationTTL:   confirmationTTL,

		ModelAdapter: modelAdapter,
		ModelBaseURL: modelBaseURL,
		ModelAPIKey:  modelAPIKey,
		ModelName:    modelName,
		ModelTimeout: modelTimeout,
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

func boolValue(lookup LookupEnv, key string, fallback bool) (bool, error) {
	value, ok := lookup(key)
	if !ok || value == "" {
		return fallback, nil
	}
	switch strings.ToLower(value) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("config: invalid %s", key)
	}
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
