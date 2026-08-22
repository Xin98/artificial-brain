package config

import (
	"strings"
	"testing"
)

// privateDeploymentEnv returns the minimal environment under which the
// private deployment mode loads for RoleAPI: a valid E.164 admin phone is
// required in private mode.
func privateDeploymentEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":            "postgres://user:secret@db/workbench",
		"APP_ENV":                 "development",
		"REMINDER_RECEIPT_SECRET": "receipt-test-secret",
		"DEPLOYMENT_MODE":         "private",
		"PRIVATE_ADMIN_PHONE":     "+8613800138000",
	}
}

func TestLoadDefaultsIter0004Fields(t *testing.T) {
	cfg, err := Load(RoleAPI, mapLookup(reminderDevEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeploymentMode != DeploymentModeCloud {
		t.Fatalf("DeploymentMode = %q, want %q", cfg.DeploymentMode, DeploymentModeCloud)
	}
	if cfg.DeploymentMode != "cloud" {
		t.Fatalf("DeploymentMode = %q, want cloud", cfg.DeploymentMode)
	}
	if cfg.PrivateAdminPhone != "" {
		t.Fatalf("PrivateAdminPhone = %q, want empty", cfg.PrivateAdminPhone)
	}
	if cfg.PortabilityMaxBundleBytes != 33554432 {
		t.Fatalf("PortabilityMaxBundleBytes = %d, want 33554432", cfg.PortabilityMaxBundleBytes)
	}
}

func TestLoadPrivateModeRequiresAdminPhoneForAPIRole(t *testing.T) {
	env := privateDeploymentEnv()
	delete(env, "PRIVATE_ADMIN_PHONE")

	_, err := Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for missing PRIVATE_ADMIN_PHONE in private mode")
	}
	if !strings.Contains(err.Error(), "PRIVATE_ADMIN_PHONE") {
		t.Fatalf("error = %q, want key PRIVATE_ADMIN_PHONE", err)
	}
}

func TestLoadPrivateModeIgnoresAdminPhoneRequirementForNonAPIRoles(t *testing.T) {
	for _, role := range []Role{RoleWorker, RoleMigrate} {
		env := privateDeploymentEnv()
		delete(env, "PRIVATE_ADMIN_PHONE")
		delete(env, "REMINDER_RECEIPT_SECRET")
		cfg, err := Load(role, mapLookup(env))
		if err != nil {
			t.Fatalf("role %s: %v", role, err)
		}
		if cfg.DeploymentMode != DeploymentModePrivate {
			t.Fatalf("role %s: DeploymentMode = %q, want %q", role, cfg.DeploymentMode, DeploymentModePrivate)
		}
		if cfg.PrivateAdminPhone != "" {
			t.Fatalf("role %s: PrivateAdminPhone = %q, want empty", role, cfg.PrivateAdminPhone)
		}
	}
}

func TestLoadPrivateModeAcceptsValidAdminPhone(t *testing.T) {
	cfg, err := Load(RoleAPI, mapLookup(privateDeploymentEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeploymentMode != DeploymentModePrivate {
		t.Fatalf("DeploymentMode = %q, want %q", cfg.DeploymentMode, DeploymentModePrivate)
	}
	if cfg.PrivateAdminPhone != "+8613800138000" {
		t.Fatalf("PrivateAdminPhone = %q, want +8613800138000", cfg.PrivateAdminPhone)
	}
}

func TestLoadRejectsInvalidAdminPhones(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "not a phone", value: "not-a-phone"},
		{name: "too short", value: "12345"},
		{name: "leading zero", value: "+0123456789"},
		{name: "too long", value: "+12345678901234567"},
		{name: "internal separator", value: "+86 13800138000"},
		// A '+'-less E.164 number would provision the admin under a string
		// the login gate never matches (logins compare the canonical '+'
		// form), permanently locking the operator out.
		{name: "missing plus", value: "8613800138000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := privateDeploymentEnv()
			env["PRIVATE_ADMIN_PHONE"] = tt.value
			_, err := Load(RoleAPI, mapLookup(env))
			if err == nil {
				t.Fatalf("expected error for PRIVATE_ADMIN_PHONE = %q", tt.value)
			}
			if !strings.Contains(err.Error(), "PRIVATE_ADMIN_PHONE") {
				t.Fatalf("error = %q, want key PRIVATE_ADMIN_PHONE", err)
			}
		})
	}
}

func TestLoadRejectsUnknownDeploymentMode(t *testing.T) {
	env := reminderDevEnv()
	env["DEPLOYMENT_MODE"] = "carrier"
	_, err := Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for DEPLOYMENT_MODE = carrier")
	}
	if !strings.Contains(err.Error(), "DEPLOYMENT_MODE") {
		t.Fatalf("error = %q, want key DEPLOYMENT_MODE", err)
	}
}

func TestLoadRejectsAdminPhoneInCloudMode(t *testing.T) {
	env := reminderDevEnv()
	env["PRIVATE_ADMIN_PHONE"] = "+8613800138000"
	_, err := Load(RoleAPI, mapLookup(env))
	if err == nil {
		t.Fatal("expected error for PRIVATE_ADMIN_PHONE set with cloud DEPLOYMENT_MODE")
	}
	if !strings.Contains(err.Error(), "PRIVATE_ADMIN_PHONE") {
		t.Fatalf("error = %q, want key PRIVATE_ADMIN_PHONE", err)
	}
}

func TestLoadPortabilityMaxBundleBytes(t *testing.T) {
	env := reminderDevEnv()
	env["PORTABILITY_MAX_BUNDLE_BYTES"] = "1048576"
	cfg, err := Load(RoleAPI, mapLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PortabilityMaxBundleBytes != 1048576 {
		t.Fatalf("PortabilityMaxBundleBytes = %d, want 1048576", cfg.PortabilityMaxBundleBytes)
	}

	tests := []struct {
		name  string
		value string
	}{
		{name: "below 1 MiB", value: "12"},
		{name: "just below minimum", value: "1048575"},
		{name: "non-numeric", value: "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := reminderDevEnv()
			env["PORTABILITY_MAX_BUNDLE_BYTES"] = tt.value
			_, err := Load(RoleAPI, mapLookup(env))
			if err == nil {
				t.Fatalf("expected error for PORTABILITY_MAX_BUNDLE_BYTES = %q", tt.value)
			}
			if !strings.Contains(err.Error(), "PORTABILITY_MAX_BUNDLE_BYTES") {
				t.Fatalf("error = %q, want key PORTABILITY_MAX_BUNDLE_BYTES", err)
			}
		})
	}
}
