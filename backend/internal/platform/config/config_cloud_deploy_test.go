package config

import (
	"strings"
	"testing"
)

// cloudDeployEnv builds a production private-mode environment that satisfies
// every fail-closed rule; individual tests override one variable at a time.
func cloudDeployEnv(overrides map[string]string) LookupEnv {
	base := map[string]string{
		"DATABASE_URL":            "postgresql://user:secret@localhost:5432/db",
		"APP_ENV":                 "production",
		"DEPLOYMENT_MODE":         "private",
		"PRIVATE_ADMIN_EMAIL":     "admin@example.com",
		"REMINDER_RECEIPT_SECRET": "receipt-secret",
		"REMINDER_EMAIL_ADAPTER":  "smtp",
		"REMINDER_SMS_ADAPTER":    "disabled",
		"REMINDER_SMTP_HOST":      "smtp.example.com",
		"REMINDER_SMTP_PORT":      "465",
		"REMINDER_SMTP_FROM":      "noreply@example.com",
		"SMTP_HOST":               "smtp.example.com",
		"SMTP_PORT":               "465",
		"SMTP_FROM":               "noreply@example.com",
	}
	for key, value := range overrides {
		base[key] = value
	}
	return func(key string) (string, bool) {
		value, ok := base[key]
		return value, ok
	}
}

func TestLoadProductionEmailAdmin(t *testing.T) {
	cfg, err := Load(RoleAPI, cloudDeployEnv(nil))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if cfg.PrivateAdminEmail != "admin@example.com" {
		t.Fatalf("PrivateAdminEmail = %q", cfg.PrivateAdminEmail)
	}
	if cfg.ReminderSmsAdapter != ReminderSmsAdapterDisabled {
		t.Fatalf("ReminderSmsAdapter = %q", cfg.ReminderSmsAdapter)
	}
	if cfg.SmtpHost != "smtp.example.com" || cfg.SmtpPort != 465 || cfg.SmtpFrom != "noreply@example.com" {
		t.Fatalf("SMTP = %q %d %q", cfg.SmtpHost, cfg.SmtpPort, cfg.SmtpFrom)
	}
}

func TestLoadPrivateRequiresSomeAdminIdentifier(t *testing.T) {
	_, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"PRIVATE_ADMIN_EMAIL": "",
	}))
	if err == nil || !strings.Contains(err.Error(), "PRIVATE_ADMIN") {
		t.Fatalf("Load = %v, want PRIVATE_ADMIN error", err)
	}
}

func TestLoadCloudRejectsAdminIdentifiers(t *testing.T) {
	_, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"DEPLOYMENT_MODE":     "cloud",
		"PRIVATE_ADMIN_PHONE": "",
	}))
	if err == nil || !strings.Contains(err.Error(), "DEPLOYMENT_MODE") {
		t.Fatalf("Load = %v, want DEPLOYMENT_MODE error", err)
	}
}

func TestLoadProductionRequiresIdentitySmtp(t *testing.T) {
	_, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"SMTP_HOST": "",
	}))
	if err == nil || !strings.Contains(err.Error(), "SMTP_HOST") {
		t.Fatalf("Load = %v, want SMTP_HOST error", err)
	}
}

func TestLoadSmtpUsernameRequiresPassword(t *testing.T) {
	_, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"SMTP_USERNAME": "noreply@example.com",
	}))
	if err == nil || !strings.Contains(err.Error(), "SMTP_PASSWORD") {
		t.Fatalf("Load = %v, want SMTP_PASSWORD error", err)
	}
}

func TestLoadInvalidAdminEmail(t *testing.T) {
	_, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"PRIVATE_ADMIN_EMAIL": "not-an-email",
	}))
	if err == nil || !strings.Contains(err.Error(), "PRIVATE_ADMIN_EMAIL") {
		t.Fatalf("Load = %v, want PRIVATE_ADMIN_EMAIL error", err)
	}
}

func TestLoadWorkerRoleSkipsIdentitySmtp(t *testing.T) {
	env := cloudDeployEnv(map[string]string{
		"SMTP_HOST": "",
		"SMTP_PORT": "",
		"SMTP_FROM": "",
	})
	cfg, err := Load(RoleWorker, env)
	if err != nil {
		t.Fatalf("Load(RoleWorker) = %v", err)
	}
	if cfg.Role != RoleWorker {
		t.Fatalf("Role = %q", cfg.Role)
	}
}

func TestLoadPrivateAdminEmailIsCaseNormalized(t *testing.T) {
	cfg, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"PRIVATE_ADMIN_EMAIL": "Admin@Example.COM",
	}))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if cfg.PrivateAdminEmail != "admin@example.com" {
		t.Fatalf("PrivateAdminEmail = %q, want the lowercase canonical form", cfg.PrivateAdminEmail)
	}
}

func TestLoadPrivatePhoneOnlyAdminStillWorks(t *testing.T) {
	cfg, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"PRIVATE_ADMIN_EMAIL": "",
		"PRIVATE_ADMIN_PHONE": "+8613800137999",
	}))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if cfg.PrivateAdminPhone != "+8613800137999" {
		t.Fatalf("PrivateAdminPhone = %q", cfg.PrivateAdminPhone)
	}
}
