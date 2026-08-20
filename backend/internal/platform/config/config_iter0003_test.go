package config

import (
	"strings"
	"testing"
	"time"
)

// reminderDevEnv returns the minimal environment under which the reminder
// defaults (fake adapters, dev outbox disabled) load: fake adapters are
// forbidden in production, and RoleAPI requires a receipt secret.
func reminderDevEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":            "postgres://user:secret@db/workbench",
		"APP_ENV":                 "development",
		"REMINDER_RECEIPT_SECRET": "receipt-test-secret",
	}
}

// reminderRealAdapterEnv selects the real adapters with every required
// setting, so the configuration loads even under the production APP_ENV
// default.
func reminderRealAdapterEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":                      "postgres://user:secret@db/workbench",
		"REMINDER_EMAIL_ADAPTER":            "smtp",
		"REMINDER_SMTP_HOST":                "smtp.example.com",
		"REMINDER_SMTP_PORT":                "587",
		"REMINDER_SMTP_FROM":                "reminders@example.com",
		"REMINDER_SMS_ADAPTER":              "aliyun",
		"REMINDER_ALIYUN_ACCESS_KEY_ID":     "ak-test",
		"REMINDER_ALIYUN_ACCESS_KEY_SECRET": "sk-test",
		"REMINDER_ALIYUN_SIGN_NAME":         "workbench",
		"REMINDER_ALIYUN_TEMPLATE_CODE":     "SMS_123456",
		"REMINDER_RECEIPT_SECRET":           "receipt-test-secret",
	}
}

func TestLoadDefaultsReminderFields(t *testing.T) {
	cfg, err := Load(RoleAPI, mapLookup(reminderDevEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReminderEmailAdapter != "fake" {
		t.Fatalf("ReminderEmailAdapter = %q, want fake", cfg.ReminderEmailAdapter)
	}
	if cfg.ReminderSmsAdapter != "fake" {
		t.Fatalf("ReminderSmsAdapter = %q, want fake", cfg.ReminderSmsAdapter)
	}
	if cfg.ReminderDevOutboxEnabled {
		t.Fatal("ReminderDevOutboxEnabled default should be false")
	}
	if cfg.ReminderReceiptSecret != "receipt-test-secret" {
		t.Fatal("ReminderReceiptSecret not loaded")
	}
	if cfg.ReminderSmtpHost != "" || cfg.ReminderSmtpPort != 0 || cfg.ReminderSmtpUsername != "" || cfg.ReminderSmtpPassword != "" || cfg.ReminderSmtpFrom != "" {
		t.Fatal("smtp settings should default to zero values")
	}
	if cfg.ReminderSmtpTimeout != 10*time.Second {
		t.Fatalf("ReminderSmtpTimeout = %s, want 10s", cfg.ReminderSmtpTimeout)
	}
	if cfg.ReminderAliyunEndpoint != "https://dysmsapi.aliyuncs.com" {
		t.Fatalf("ReminderAliyunEndpoint = %q", cfg.ReminderAliyunEndpoint)
	}
	if cfg.ReminderAliyunAccessKeyID != "" || cfg.ReminderAliyunAccessKeySecret != "" || cfg.ReminderAliyunSignName != "" || cfg.ReminderAliyunTemplateCode != "" {
		t.Fatal("aliyun settings should default to zero values")
	}
	if cfg.ReminderQueueEmailConcurrency != 5 {
		t.Fatalf("ReminderQueueEmailConcurrency = %d, want 5", cfg.ReminderQueueEmailConcurrency)
	}
	if cfg.ReminderQueueSmsConcurrency != 5 {
		t.Fatalf("ReminderQueueSmsConcurrency = %d, want 5", cfg.ReminderQueueSmsConcurrency)
	}
	if cfg.ReminderJobMaxAttempts != 5 {
		t.Fatalf("ReminderJobMaxAttempts = %d, want 5", cfg.ReminderJobMaxAttempts)
	}
}

func TestLoadRejectsUnknownReminderAdapters(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "unknown email adapter", key: "REMINDER_EMAIL_ADAPTER", value: "carrier-pigeon"},
		{name: "unknown sms adapter", key: "REMINDER_SMS_ADAPTER", value: "smoke-signal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := reminderDevEnv()
			env[tt.key] = tt.value
			_, err := Load(RoleAPI, mapLookup(env))
			if err == nil {
				t.Fatalf("expected error for %s = %q", tt.key, tt.value)
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("error = %q, want key %q", err, tt.key)
			}
		})
	}
}

func TestLoadSmtpAdapterRequiresSmtpSettings(t *testing.T) {
	env := reminderDevEnv()
	env["REMINDER_EMAIL_ADAPTER"] = "smtp"

	_, err := Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for smtp without REMINDER_SMTP_HOST")
	}
	if !strings.Contains(err.Error(), "REMINDER_SMTP_HOST") {
		t.Fatalf("error = %q, want key REMINDER_SMTP_HOST", err)
	}

	env["REMINDER_SMTP_HOST"] = "smtp.example.com"
	_, err = Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for smtp without REMINDER_SMTP_PORT")
	}
	if !strings.Contains(err.Error(), "REMINDER_SMTP_PORT") {
		t.Fatalf("error = %q, want key REMINDER_SMTP_PORT", err)
	}

	env["REMINDER_SMTP_PORT"] = "587"
	_, err = Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for smtp without REMINDER_SMTP_FROM")
	}
	if !strings.Contains(err.Error(), "REMINDER_SMTP_FROM") {
		t.Fatalf("error = %q, want key REMINDER_SMTP_FROM", err)
	}

	env["REMINDER_SMTP_FROM"] = "reminders@example.com"
	cfg, err := Load(RoleAPI, mapLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReminderSmtpHost != "smtp.example.com" || cfg.ReminderSmtpPort != 587 || cfg.ReminderSmtpFrom != "reminders@example.com" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.ReminderSmtpUsername != "" || cfg.ReminderSmtpPassword != "" {
		t.Fatal("smtp credentials should stay empty when unset")
	}
}

func TestLoadAliyunAdapterRequiresAliyunSettings(t *testing.T) {
	env := reminderDevEnv()
	env["REMINDER_SMS_ADAPTER"] = "aliyun"

	_, err := Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for aliyun without REMINDER_ALIYUN_ACCESS_KEY_ID")
	}
	if !strings.Contains(err.Error(), "REMINDER_ALIYUN_ACCESS_KEY_ID") {
		t.Fatalf("error = %q, want key REMINDER_ALIYUN_ACCESS_KEY_ID", err)
	}

	env["REMINDER_ALIYUN_ACCESS_KEY_ID"] = "ak-test"
	_, err = Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for aliyun without REMINDER_ALIYUN_ACCESS_KEY_SECRET")
	}
	if !strings.Contains(err.Error(), "REMINDER_ALIYUN_ACCESS_KEY_SECRET") {
		t.Fatalf("error = %q, want key REMINDER_ALIYUN_ACCESS_KEY_SECRET", err)
	}

	env["REMINDER_ALIYUN_ACCESS_KEY_SECRET"] = "sk-test"
	_, err = Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for aliyun without REMINDER_ALIYUN_SIGN_NAME")
	}
	if !strings.Contains(err.Error(), "REMINDER_ALIYUN_SIGN_NAME") {
		t.Fatalf("error = %q, want key REMINDER_ALIYUN_SIGN_NAME", err)
	}

	env["REMINDER_ALIYUN_SIGN_NAME"] = "workbench"
	_, err = Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for aliyun without REMINDER_ALIYUN_TEMPLATE_CODE")
	}
	if !strings.Contains(err.Error(), "REMINDER_ALIYUN_TEMPLATE_CODE") {
		t.Fatalf("error = %q, want key REMINDER_ALIYUN_TEMPLATE_CODE", err)
	}

	env["REMINDER_ALIYUN_TEMPLATE_CODE"] = "SMS_123456"
	cfg, err := Load(RoleAPI, mapLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReminderAliyunEndpoint != "https://dysmsapi.aliyuncs.com" {
		t.Fatalf("ReminderAliyunEndpoint = %q", cfg.ReminderAliyunEndpoint)
	}
	if cfg.ReminderAliyunAccessKeyID != "ak-test" || cfg.ReminderAliyunSignName != "workbench" || cfg.ReminderAliyunTemplateCode != "SMS_123456" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadRejectsFakeAdaptersInProduction(t *testing.T) {
	emailEnv := map[string]string{
		"DATABASE_URL":            "postgres://user:secret@db/workbench",
		"REMINDER_EMAIL_ADAPTER":  "fake",
		"REMINDER_RECEIPT_SECRET": "receipt-test-secret",
	}
	// APP_ENV unset defaults to production.
	_, err := Load(RoleAPI, mapLookup(emailEnv))
	if err == nil {
		t.Fatal("expected error for fake email adapter with production APP_ENV")
	}
	if !strings.Contains(err.Error(), "REMINDER_EMAIL_ADAPTER") {
		t.Fatalf("error = %q, want key REMINDER_EMAIL_ADAPTER", err)
	}

	smsEnv := map[string]string{
		"DATABASE_URL":            "postgres://user:secret@db/workbench",
		"REMINDER_EMAIL_ADAPTER":  "smtp",
		"REMINDER_SMTP_HOST":      "smtp.example.com",
		"REMINDER_SMTP_PORT":      "587",
		"REMINDER_SMTP_FROM":      "reminders@example.com",
		"REMINDER_SMS_ADAPTER":    "fake",
		"REMINDER_RECEIPT_SECRET": "receipt-test-secret",
	}
	_, err = Load(RoleAPI, mapLookup(smsEnv))
	if err == nil {
		t.Fatal("expected error for fake sms adapter with production APP_ENV")
	}
	if !strings.Contains(err.Error(), "REMINDER_SMS_ADAPTER") {
		t.Fatalf("error = %q, want key REMINDER_SMS_ADAPTER", err)
	}
}

func TestLoadAcceptsRealAdaptersInProduction(t *testing.T) {
	cfg, err := Load(RoleAPI, mapLookup(reminderRealAdapterEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppEnv != "production" {
		t.Fatalf("AppEnv = %q, want production", cfg.AppEnv)
	}
	if cfg.ReminderEmailAdapter != "smtp" || cfg.ReminderSmsAdapter != "aliyun" {
		t.Fatalf("adapters = %q/%q, want smtp/aliyun", cfg.ReminderEmailAdapter, cfg.ReminderSmsAdapter)
	}
}

func TestLoadRejectsReminderDevOutboxInProduction(t *testing.T) {
	env := reminderRealAdapterEnv()
	env["REMINDER_DEV_OUTBOX_ENABLED"] = "true"
	_, err := Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for REMINDER_DEV_OUTBOX_ENABLED with production APP_ENV")
	}
	if !strings.Contains(err.Error(), "REMINDER_DEV_OUTBOX_ENABLED") {
		t.Fatalf("error = %q, want key REMINDER_DEV_OUTBOX_ENABLED", err)
	}
}

func TestLoadAcceptsReminderDevOutboxInDevelopment(t *testing.T) {
	env := reminderDevEnv()
	env["REMINDER_DEV_OUTBOX_ENABLED"] = "true"
	cfg, err := Load(RoleAPI, mapLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ReminderDevOutboxEnabled {
		t.Fatal("ReminderDevOutboxEnabled = false, want true")
	}
}

func TestLoadRejectsInvalidReminderDevOutboxValue(t *testing.T) {
	env := reminderDevEnv()
	env["REMINDER_DEV_OUTBOX_ENABLED"] = "maybe"
	_, err := Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for invalid REMINDER_DEV_OUTBOX_ENABLED")
	}
	if !strings.Contains(err.Error(), "REMINDER_DEV_OUTBOX_ENABLED") {
		t.Fatalf("error = %q, want key REMINDER_DEV_OUTBOX_ENABLED", err)
	}
}

func TestLoadReceiptSecretRequiredForAPIRoleOnly(t *testing.T) {
	_, err := Load(RoleAPI, mapLookup(map[string]string{
		"DATABASE_URL": "postgres://user:secret@db/workbench",
		"APP_ENV":      "development",
	}))
	if err == nil {
		t.Fatal("expected error for missing REMINDER_RECEIPT_SECRET")
	}
	if !strings.Contains(err.Error(), "REMINDER_RECEIPT_SECRET") {
		t.Fatalf("error = %q, want key REMINDER_RECEIPT_SECRET", err)
	}

	for _, role := range []Role{RoleWorker, RoleMigrate} {
		cfg, err := Load(role, mapLookup(map[string]string{
			"DATABASE_URL": "postgres://user:secret@db/workbench",
			"APP_ENV":      "development",
		}))
		if err != nil {
			t.Fatalf("role %s: %v", role, err)
		}
		if cfg.ReminderReceiptSecret != "" {
			t.Fatalf("role %s: ReminderReceiptSecret = %q, want empty", role, cfg.ReminderReceiptSecret)
		}
	}
}

func TestLoadRejectsInvalidReminderIntegers(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "zero email concurrency", key: "REMINDER_QUEUE_EMAIL_CONCURRENCY", value: "0"},
		{name: "non-numeric email concurrency", key: "REMINDER_QUEUE_EMAIL_CONCURRENCY", value: "x"},
		{name: "zero sms concurrency", key: "REMINDER_QUEUE_SMS_CONCURRENCY", value: "0"},
		{name: "non-numeric sms concurrency", key: "REMINDER_QUEUE_SMS_CONCURRENCY", value: "x"},
		{name: "zero max attempts", key: "REMINDER_JOB_MAX_ATTEMPTS", value: "0"},
		{name: "non-numeric max attempts", key: "REMINDER_JOB_MAX_ATTEMPTS", value: "x"},
		{name: "invalid smtp timeout", key: "REMINDER_SMTP_TIMEOUT", value: "soon"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := reminderDevEnv()
			env[tt.key] = tt.value
			_, err := Load(RoleAPI, mapLookup(env))
			if err == nil {
				t.Fatalf("expected error for %s = %q", tt.key, tt.value)
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("error = %q, want key %q", err, tt.key)
			}
		})
	}
}

func TestLoadRejectsInvalidSmtpPort(t *testing.T) {
	for _, value := range []string{"0", "-1", "x"} {
		t.Run(value, func(t *testing.T) {
			env := reminderDevEnv()
			env["REMINDER_EMAIL_ADAPTER"] = "smtp"
			env["REMINDER_SMTP_HOST"] = "smtp.example.com"
			env["REMINDER_SMTP_PORT"] = value
			env["REMINDER_SMTP_FROM"] = "reminders@example.com"
			_, err := Load(RoleAPI, mapLookup(env))
			if err == nil {
				t.Fatalf("expected error for REMINDER_SMTP_PORT = %q", value)
			}
			if !strings.Contains(err.Error(), "REMINDER_SMTP_PORT") {
				t.Fatalf("error = %q, want key REMINDER_SMTP_PORT", err)
			}
		})
	}
}

func TestLoadRejectsInvalidAliyunEndpoint(t *testing.T) {
	env := reminderDevEnv()
	env["REMINDER_SMS_ADAPTER"] = "aliyun"
	env["REMINDER_ALIYUN_ENDPOINT"] = "dysmsapi.aliyuncs.com"
	env["REMINDER_ALIYUN_ACCESS_KEY_ID"] = "ak-test"
	env["REMINDER_ALIYUN_ACCESS_KEY_SECRET"] = "sk-test"
	env["REMINDER_ALIYUN_SIGN_NAME"] = "workbench"
	env["REMINDER_ALIYUN_TEMPLATE_CODE"] = "SMS_123456"
	_, err := Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for REMINDER_ALIYUN_ENDPOINT without a scheme")
	}
	if !strings.Contains(err.Error(), "REMINDER_ALIYUN_ENDPOINT") {
		t.Fatalf("error = %q, want key REMINDER_ALIYUN_ENDPOINT", err)
	}
}

func TestLoadReminderErrorsNeverEchoSecretValues(t *testing.T) {
	secrets := []string{
		"top-secret-receipt-value",
		"top-secret-smtp-value",
		"top-secret-aliyun-value",
	}
	secretEnv := func(extra map[string]string) map[string]string {
		env := map[string]string{
			"DATABASE_URL":                      "postgres://user@db/workbench",
			"APP_ENV":                           "development",
			"REMINDER_RECEIPT_SECRET":           "top-secret-receipt-value",
			"REMINDER_SMTP_PASSWORD":            "top-secret-smtp-value",
			"REMINDER_ALIYUN_ACCESS_KEY_SECRET": "top-secret-aliyun-value",
		}
		for key, value := range extra {
			env[key] = value
		}
		return env
	}

	tests := []struct {
		name string
		role Role
		env  map[string]string
	}{
		{
			name: "smtp missing from",
			role: RoleAPI,
			env: secretEnv(map[string]string{
				"REMINDER_EMAIL_ADAPTER": "smtp",
				"REMINDER_SMTP_HOST":     "smtp.example.com",
				"REMINDER_SMTP_PORT":     "587",
			}),
		},
		{
			name: "aliyun missing template code",
			role: RoleAPI,
			env: secretEnv(map[string]string{
				"REMINDER_SMS_ADAPTER":          "aliyun",
				"REMINDER_ALIYUN_ACCESS_KEY_ID": "ak-test",
				"REMINDER_ALIYUN_SIGN_NAME":     "workbench",
			}),
		},
		{
			name: "invalid max attempts",
			role: RoleAPI,
			env: secretEnv(map[string]string{
				"REMINDER_JOB_MAX_ATTEMPTS": "x",
			}),
		},
		{
			name: "fake email adapter in production",
			role: RoleWorker,
			env: map[string]string{
				"DATABASE_URL":                      "postgres://user@db/workbench",
				"REMINDER_EMAIL_ADAPTER":            "fake",
				"REMINDER_SMTP_PASSWORD":            "top-secret-smtp-value",
				"REMINDER_ALIYUN_ACCESS_KEY_SECRET": "top-secret-aliyun-value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(tt.role, mapLookup(tt.env))
			if err == nil {
				t.Fatal("expected error")
			}
			for _, secret := range secrets {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("secret leaked in error: %v", err)
				}
			}
		})
	}
}
