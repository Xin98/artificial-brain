package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
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

	defaultIdentitySmtpTimeout = 10 * time.Second

	ModelAdapterDeterministic    = "deterministic"
	ModelAdapterOpenAICompatible = "openai_compatible"
	AppEnvProduction             = "production"
)

const (
	defaultDeploymentMode            = "cloud"
	defaultPortabilityMaxBundleBytes = 33554432
	minimumPortabilityBundleBytes    = 1048576

	DeploymentModeCloud   = "cloud"
	DeploymentModePrivate = "private"
)

// e164PhonePattern duplicates the identity module's E.164 validation
// deliberately: the platform package must not import business modules
// (ITER-0004 assumption A1).
var e164PhonePattern = regexp.MustCompile(`^\+?[1-9][0-9]{6,14}$`)

// adminEmailPattern duplicates the identity module's email validation
// deliberately: the platform package must not import business modules
// (ITER-0004 assumption A1).
var adminEmailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

const (
	defaultReminderEmailAdapter          = "fake"
	defaultReminderSmsAdapter            = "fake"
	defaultReminderSmtpTimeout           = 10 * time.Second
	defaultReminderAliyunEndpoint        = "https://dysmsapi.aliyuncs.com"
	defaultReminderQueueEmailConcurrency = 5
	defaultReminderQueueSmsConcurrency   = 5
	defaultReminderJobMaxAttempts        = 5

	ReminderEmailAdapterFake = "fake"
	ReminderEmailAdapterSmtp = "smtp"
	ReminderSmsAdapterFake   = "fake"
	ReminderSmsAdapterAliyun = "aliyun"
	ReminderSmsAdapterDisabled = "disabled"
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

	ReminderEmailAdapter          string
	ReminderSmsAdapter            string
	ReminderReceiptSecret         string
	ReminderDevOutboxEnabled      bool
	ReminderSmtpHost              string
	ReminderSmtpPort              int
	ReminderSmtpUsername          string
	ReminderSmtpPassword          string
	ReminderSmtpFrom              string
	ReminderSmtpTimeout           time.Duration
	ReminderAliyunEndpoint        string
	ReminderAliyunAccessKeyID     string
	ReminderAliyunAccessKeySecret string
	ReminderAliyunSignName        string
	ReminderAliyunTemplateCode    string
	ReminderQueueEmailConcurrency int
	ReminderQueueSmsConcurrency   int
	ReminderJobMaxAttempts        int

	DeploymentMode            string
	PrivateAdminPhone         string
	PrivateAdminEmail         string

	SmtpHost     string
	SmtpPort     int
	SmtpUsername string
	SmtpPassword string
	SmtpFrom     string
	SmtpTimeout  time.Duration

	PortabilityMaxBundleBytes int
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

	reminderEmailAdapter := valueOrDefault(lookup, "REMINDER_EMAIL_ADAPTER", defaultReminderEmailAdapter)
	switch reminderEmailAdapter {
	case ReminderEmailAdapterFake, ReminderEmailAdapterSmtp:
	default:
		return Config{}, fmt.Errorf("config: invalid REMINDER_EMAIL_ADAPTER")
	}
	reminderSmsAdapter := valueOrDefault(lookup, "REMINDER_SMS_ADAPTER", defaultReminderSmsAdapter)
	switch reminderSmsAdapter {
	case ReminderSmsAdapterFake, ReminderSmsAdapterAliyun, ReminderSmsAdapterDisabled:
	default:
		return Config{}, fmt.Errorf("config: invalid REMINDER_SMS_ADAPTER")
	}

	reminderDevOutboxEnabled, err := boolValue(lookup, "REMINDER_DEV_OUTBOX_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	if reminderDevOutboxEnabled && appEnv == AppEnvProduction {
		return Config{}, fmt.Errorf("config: REMINDER_DEV_OUTBOX_ENABLED requires a non-production APP_ENV")
	}
	if reminderEmailAdapter == ReminderEmailAdapterFake && appEnv == AppEnvProduction {
		return Config{}, fmt.Errorf("config: REMINDER_EMAIL_ADAPTER requires a non-production APP_ENV")
	}
	if reminderSmsAdapter == ReminderSmsAdapterFake && appEnv == AppEnvProduction {
		return Config{}, fmt.Errorf("config: REMINDER_SMS_ADAPTER requires a non-production APP_ENV")
	}

	reminderSmtpHost := valueOrDefault(lookup, "REMINDER_SMTP_HOST", "")
	reminderSmtpPort, err := intValue(lookup, "REMINDER_SMTP_PORT", 0)
	if err != nil {
		return Config{}, err
	}
	reminderSmtpUsername := valueOrDefault(lookup, "REMINDER_SMTP_USERNAME", "")
	reminderSmtpPassword := valueOrDefault(lookup, "REMINDER_SMTP_PASSWORD", "")
	reminderSmtpFrom := valueOrDefault(lookup, "REMINDER_SMTP_FROM", "")
	reminderSmtpTimeout, err := duration(lookup, "REMINDER_SMTP_TIMEOUT", defaultReminderSmtpTimeout)
	if err != nil {
		return Config{}, err
	}
	if reminderEmailAdapter == ReminderEmailAdapterSmtp {
		if reminderSmtpHost == "" {
			return Config{}, fmt.Errorf("config: missing REMINDER_SMTP_HOST")
		}
		if reminderSmtpPort < 1 {
			return Config{}, fmt.Errorf("config: invalid REMINDER_SMTP_PORT")
		}
		if reminderSmtpFrom == "" {
			return Config{}, fmt.Errorf("config: missing REMINDER_SMTP_FROM")
		}
	}

	smtpHost := valueOrDefault(lookup, "SMTP_HOST", "")
	smtpPort, err := intValue(lookup, "SMTP_PORT", 0)
	if err != nil {
		return Config{}, err
	}
	smtpUsername := valueOrDefault(lookup, "SMTP_USERNAME", "")
	smtpPassword := valueOrDefault(lookup, "SMTP_PASSWORD", "")
	smtpFrom := valueOrDefault(lookup, "SMTP_FROM", "")
	smtpTimeout, err := duration(lookup, "SMTP_TIMEOUT", defaultIdentitySmtpTimeout)
	if err != nil {
		return Config{}, err
	}
	if smtpUsername != "" && smtpPassword == "" {
		return Config{}, fmt.Errorf("config: SMTP_USERNAME requires SMTP_PASSWORD")
	}
	if appEnv == AppEnvProduction && role == RoleAPI {
		if smtpHost == "" {
			return Config{}, fmt.Errorf("config: missing SMTP_HOST")
		}
		if smtpPort < 1 {
			return Config{}, fmt.Errorf("config: invalid SMTP_PORT")
		}
		if smtpFrom == "" {
			return Config{}, fmt.Errorf("config: missing SMTP_FROM")
		}
	}

	reminderAliyunEndpoint := valueOrDefault(lookup, "REMINDER_ALIYUN_ENDPOINT", defaultReminderAliyunEndpoint)
	reminderAliyunAccessKeyID := valueOrDefault(lookup, "REMINDER_ALIYUN_ACCESS_KEY_ID", "")
	reminderAliyunAccessKeySecret := valueOrDefault(lookup, "REMINDER_ALIYUN_ACCESS_KEY_SECRET", "")
	reminderAliyunSignName := valueOrDefault(lookup, "REMINDER_ALIYUN_SIGN_NAME", "")
	reminderAliyunTemplateCode := valueOrDefault(lookup, "REMINDER_ALIYUN_TEMPLATE_CODE", "")
	if reminderSmsAdapter == ReminderSmsAdapterAliyun {
		endpoint, err := url.Parse(reminderAliyunEndpoint)
		if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
			return Config{}, fmt.Errorf("config: invalid REMINDER_ALIYUN_ENDPOINT")
		}
		if reminderAliyunAccessKeyID == "" {
			return Config{}, fmt.Errorf("config: missing REMINDER_ALIYUN_ACCESS_KEY_ID")
		}
		if reminderAliyunAccessKeySecret == "" {
			return Config{}, fmt.Errorf("config: missing REMINDER_ALIYUN_ACCESS_KEY_SECRET")
		}
		if reminderAliyunSignName == "" {
			return Config{}, fmt.Errorf("config: missing REMINDER_ALIYUN_SIGN_NAME")
		}
		if reminderAliyunTemplateCode == "" {
			return Config{}, fmt.Errorf("config: missing REMINDER_ALIYUN_TEMPLATE_CODE")
		}
	}

	reminderReceiptSecret := valueOrDefault(lookup, "REMINDER_RECEIPT_SECRET", "")
	if role == RoleAPI && reminderReceiptSecret == "" {
		return Config{}, fmt.Errorf("config: missing REMINDER_RECEIPT_SECRET")
	}

	reminderQueueEmailConcurrency, err := intValue(lookup, "REMINDER_QUEUE_EMAIL_CONCURRENCY", defaultReminderQueueEmailConcurrency)
	if err != nil {
		return Config{}, err
	}
	if reminderQueueEmailConcurrency < 1 {
		return Config{}, fmt.Errorf("config: invalid REMINDER_QUEUE_EMAIL_CONCURRENCY")
	}
	reminderQueueSmsConcurrency, err := intValue(lookup, "REMINDER_QUEUE_SMS_CONCURRENCY", defaultReminderQueueSmsConcurrency)
	if err != nil {
		return Config{}, err
	}
	if reminderQueueSmsConcurrency < 1 {
		return Config{}, fmt.Errorf("config: invalid REMINDER_QUEUE_SMS_CONCURRENCY")
	}
	reminderJobMaxAttempts, err := intValue(lookup, "REMINDER_JOB_MAX_ATTEMPTS", defaultReminderJobMaxAttempts)
	if err != nil {
		return Config{}, err
	}
	if reminderJobMaxAttempts < 1 {
		return Config{}, fmt.Errorf("config: invalid REMINDER_JOB_MAX_ATTEMPTS")
	}

	deploymentMode := valueOrDefault(lookup, "DEPLOYMENT_MODE", defaultDeploymentMode)
	switch deploymentMode {
	case DeploymentModeCloud, DeploymentModePrivate:
	default:
		return Config{}, fmt.Errorf("config: invalid DEPLOYMENT_MODE")
	}
	privateAdminPhone := valueOrDefault(lookup, "PRIVATE_ADMIN_PHONE", "")
	privateAdminEmail := valueOrDefault(lookup, "PRIVATE_ADMIN_EMAIL", "")
	if deploymentMode == DeploymentModeCloud && (privateAdminPhone != "" || privateAdminEmail != "") {
		return Config{}, fmt.Errorf("config: PRIVATE_ADMIN_PHONE and PRIVATE_ADMIN_EMAIL require a private DEPLOYMENT_MODE")
	}
	if privateAdminPhone != "" && !e164PhonePattern.MatchString(privateAdminPhone) {
		return Config{}, fmt.Errorf("config: invalid PRIVATE_ADMIN_PHONE")
	}
	// The E.164 pattern allows an optional '+', but the admin phone gates
	// provisioning and login by exact string comparison: a '+'-less value
	// would provision the user under one string while the admin logs in with
	// the canonical '+' form, a permanent lockout. Require the canonical
	// form so both sides always compare identical strings.
	if privateAdminPhone != "" && !strings.HasPrefix(privateAdminPhone, "+") {
		return Config{}, fmt.Errorf("config: PRIVATE_ADMIN_PHONE must start with '+' (canonical E.164 form)")
	}
	if privateAdminEmail != "" && !adminEmailPattern.MatchString(privateAdminEmail) {
		return Config{}, fmt.Errorf("config: invalid PRIVATE_ADMIN_EMAIL")
	}
	if deploymentMode == DeploymentModePrivate && role == RoleAPI && privateAdminPhone == "" && privateAdminEmail == "" {
		return Config{}, fmt.Errorf("config: private DEPLOYMENT_MODE requires PRIVATE_ADMIN_PHONE or PRIVATE_ADMIN_EMAIL")
	}

	portabilityMaxBundleBytes, err := intValue(lookup, "PORTABILITY_MAX_BUNDLE_BYTES", defaultPortabilityMaxBundleBytes)
	if err != nil {
		return Config{}, err
	}
	if portabilityMaxBundleBytes < minimumPortabilityBundleBytes {
		return Config{}, fmt.Errorf("config: invalid PORTABILITY_MAX_BUNDLE_BYTES")
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

		ReminderEmailAdapter:          reminderEmailAdapter,
		ReminderSmsAdapter:            reminderSmsAdapter,
		ReminderReceiptSecret:         reminderReceiptSecret,
		ReminderDevOutboxEnabled:      reminderDevOutboxEnabled,
		ReminderSmtpHost:              reminderSmtpHost,
		ReminderSmtpPort:              reminderSmtpPort,
		ReminderSmtpUsername:          reminderSmtpUsername,
		ReminderSmtpPassword:          reminderSmtpPassword,
		ReminderSmtpFrom:              reminderSmtpFrom,
		ReminderSmtpTimeout:           reminderSmtpTimeout,
		ReminderAliyunEndpoint:        reminderAliyunEndpoint,
		ReminderAliyunAccessKeyID:     reminderAliyunAccessKeyID,
		ReminderAliyunAccessKeySecret: reminderAliyunAccessKeySecret,
		ReminderAliyunSignName:        reminderAliyunSignName,
		ReminderAliyunTemplateCode:    reminderAliyunTemplateCode,
		ReminderQueueEmailConcurrency: reminderQueueEmailConcurrency,
		ReminderQueueSmsConcurrency:   reminderQueueSmsConcurrency,
		ReminderJobMaxAttempts:        reminderJobMaxAttempts,

		DeploymentMode:            deploymentMode,
		PrivateAdminPhone:         privateAdminPhone,
		PrivateAdminEmail:         privateAdminEmail,
		SmtpHost:          smtpHost,
		SmtpPort:          smtpPort,
		SmtpUsername:      smtpUsername,
		SmtpPassword:      smtpPassword,
		SmtpFrom:          smtpFrom,
		SmtpTimeout:       smtpTimeout,
		PortabilityMaxBundleBytes: portabilityMaxBundleBytes,
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

func intValue(lookup LookupEnv, key string, fallback int) (int, error) {
	value, ok := lookup(key)
	if !ok || value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
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
