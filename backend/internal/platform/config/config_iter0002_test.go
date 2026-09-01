package config

import (
	"strings"
	"testing"
	"time"
)

func baseEnv() map[string]string {
	// Real adapters (plus an API receipt secret) keep this base loadable
	// under the production APP_ENV default: fake reminder adapters are
	// forbidden in production.
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
		"REMINDER_RECEIPT_SECRET":           "receipt-secret",
		"SMTP_HOST":                         "smtp.example.com",
		"SMTP_PORT":                         "587",
		"SMTP_FROM":                         "noreply@example.com",
	}
}

func TestLoadDefaultsIdentityAndModelFields(t *testing.T) {
	cfg, err := Load(RoleAPI, mapLookup(baseEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppEnv != "production" {
		t.Fatalf("AppEnv = %q, want production", cfg.AppEnv)
	}
	if cfg.DevInboxEnabled {
		t.Fatal("DevInboxEnabled default should be false")
	}
	if cfg.SessionTTL != 168*time.Hour {
		t.Fatalf("SessionTTL = %s", cfg.SessionTTL)
	}
	if cfg.LoginChallengeTTL != 5*time.Minute {
		t.Fatalf("LoginChallengeTTL = %s", cfg.LoginChallengeTTL)
	}
	if cfg.ChannelCodeTTL != 10*time.Minute {
		t.Fatalf("ChannelCodeTTL = %s", cfg.ChannelCodeTTL)
	}
	if cfg.ConfirmationTTL != 5*time.Minute {
		t.Fatalf("ConfirmationTTL = %s", cfg.ConfirmationTTL)
	}
	if cfg.ModelAdapter != "deterministic" {
		t.Fatalf("ModelAdapter = %q, want deterministic", cfg.ModelAdapter)
	}
	if cfg.ModelTimeout != 15*time.Second {
		t.Fatalf("ModelTimeout = %s", cfg.ModelTimeout)
	}
}

func TestLoadRejectsDevInboxInProduction(t *testing.T) {
	env := baseEnv()
	env["DEV_INBOX_ENABLED"] = "true"
	if _, err := Load(RoleAPI, mapLookup(env)); err == nil {
		t.Fatal("expected error for DEV_INBOX_ENABLED with production APP_ENV")
	}
}

func TestLoadAcceptsDevInboxInDevelopment(t *testing.T) {
	env := baseEnv()
	env["DEV_INBOX_ENABLED"] = "true"
	env["APP_ENV"] = "development"
	cfg, err := Load(RoleAPI, mapLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DevInboxEnabled || cfg.AppEnv != "development" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadRejectsInvalidDevInboxValue(t *testing.T) {
	env := baseEnv()
	env["DEV_INBOX_ENABLED"] = "maybe"
	if _, err := Load(RoleAPI, mapLookup(env)); err == nil {
		t.Fatal("expected error for invalid DEV_INBOX_ENABLED")
	}
}

func TestLoadOpenAIAdapterRequiresModelSettings(t *testing.T) {
	env := baseEnv()
	env["MODEL_ADAPTER"] = "openai_compatible"
	if _, err := Load(RoleAPI, mapLookup(env)); err == nil {
		t.Fatal("expected error for openai_compatible without MODEL_BASE_URL")
	}

	env["MODEL_BASE_URL"] = "http://model.local/v1"
	if _, err := Load(RoleAPI, mapLookup(env)); err == nil {
		t.Fatal("expected error for openai_compatible without MODEL_NAME")
	}

	env["MODEL_NAME"] = "corp-model"
	if _, err := Load(RoleAPI, mapLookup(env)); err == nil {
		t.Fatal("expected error for openai_compatible without MODEL_API_KEY")
	}

	env["MODEL_API_KEY"] = "sk-test"
	cfg, err := Load(RoleAPI, mapLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModelAdapter != "openai_compatible" || cfg.ModelBaseURL != "http://model.local/v1" || cfg.ModelName != "corp-model" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadRejectsUnknownModelAdapter(t *testing.T) {
	env := baseEnv()
	env["MODEL_ADAPTER"] = "carrier-pigeon"
	if _, err := Load(RoleAPI, mapLookup(env)); err == nil {
		t.Fatal("expected error for unknown MODEL_ADAPTER")
	}
}

func TestLoadConfigErrorsNeverEchoSecretValues(t *testing.T) {
	env := baseEnv()
	env["MODEL_ADAPTER"] = "openai_compatible"
	env["MODEL_API_KEY"] = "super-secret-key"
	env["MODEL_NAME"] = "corp-model"
	// MODEL_BASE_URL missing triggers an error; it must not echo the API key.
	_, err := Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for missing MODEL_BASE_URL")
	}
	if strings.Contains(err.Error(), "super-secret-key") {
		t.Fatal("config error echoed the model API key")
	}
}
