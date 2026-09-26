package config

import "testing"

func iter0005Env(extra map[string]string) map[string]string {
	env := map[string]string{
		"DATABASE_URL": "postgres://user:secret@db/workbench",
		"APP_ENV":      "development",
		// The API role requires a receipt secret.
		"REMINDER_RECEIPT_SECRET": "receipt-secret",
	}
	for key, value := range extra {
		env[key] = value
	}
	return env
}

func TestConversationHistoryTurnsDefaults(t *testing.T) {
	cfg, err := Load(RoleAPI, mapLookup(iter0005Env(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConversationHistoryTurns != 10 {
		t.Fatalf("ConversationHistoryTurns = %d, want default 10", cfg.ConversationHistoryTurns)
	}

	// An explicitly empty value falls back like the other typed helpers.
	cfg, err = Load(RoleAPI, mapLookup(iter0005Env(map[string]string{
		"CONVERSATION_HISTORY_TURNS": "",
	})))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConversationHistoryTurns != 10 {
		t.Fatalf("ConversationHistoryTurns(empty) = %d, want default 10", cfg.ConversationHistoryTurns)
	}
}

func TestConversationHistoryTurnsBounds(t *testing.T) {
	for _, value := range []string{"0", "1", "10", "50"} {
		cfg, err := Load(RoleAPI, mapLookup(iter0005Env(map[string]string{
			"CONVERSATION_HISTORY_TURNS": value,
		})))
		if err != nil {
			t.Fatalf("Load(%s) error = %v", value, err)
		}
		want := 0
		switch value {
		case "1":
			want = 1
		case "10":
			want = 10
		case "50":
			want = 50
		}
		if cfg.ConversationHistoryTurns != want {
			t.Fatalf("Load(%s) turns = %d, want %d", value, cfg.ConversationHistoryTurns, want)
		}
	}
}

func TestConversationHistoryTurnsFailClosed(t *testing.T) {
	for _, value := range []string{"51", "-1", "garbage"} {
		if _, err := Load(RoleAPI, mapLookup(iter0005Env(map[string]string{
			"CONVERSATION_HISTORY_TURNS": value,
		}))); err == nil {
			t.Fatalf("Load(%q) accepted an out-of-bounds value", value)
		}
	}
}
