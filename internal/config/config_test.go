package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func validEnv() []string {
	return []string{
		"DISCORD_TOKEN=token", "DISCORD_GUILD_ID=guild", "DISCORD_USER_ID=user", "DISCORD_CHANNEL_ID=channel",
		"OPENAI_BASE_URL=https://provider.example/v1", "OPENAI_MODEL=model", "SQLITE_PATH=./data.db",
	}
}

func TestLoadDefaultsAndFixedTimezone(t *testing.T) {
	cfg, err := LoadFile(filepath.Join(t.TempDir(), ".env"), validEnv())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReminderEnabled || cfg.ReminderTime != DefaultReminderTime || cfg.ReminderCooldown != DefaultReminderCooldown {
		t.Fatalf("unexpected reminder defaults: %+v", cfg)
	}
	if cfg.OpenAIAPIKey != "" || cfg.Location.String() != ApplicationTimezone {
		t.Fatalf("unexpected optional settings: %+v", cfg)
	}
	if _, offset := time.Now().In(cfg.Location).Zone(); offset != 7*60*60 {
		t.Fatalf("unexpected timezone offset: %d", offset)
	}
}

func TestLoadDotEnvAndEnvironmentOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("OPENAI_MODEL=from-file\nREMINDER_ENABLED=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(validEnv(), "OPENAI_MODEL=from-env")
	cfg, err := LoadFile(path, env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenAIModel != "from-env" || !cfg.ReminderEnabled {
		t.Fatalf("dotenv merge failed: %+v", cfg)
	}
}

func TestLoadRejectsMissingAndInvalidSettings(t *testing.T) {
	if _, err := LoadFile(filepath.Join(t.TempDir(), "missing"), nil); err == nil {
		t.Fatal("expected missing required configuration")
	}
	for _, env := range [][]string{
		append(validEnv(), "OPENAI_BASE_URL=not-a-url"),
		append(validEnv(), "REMINDER_TIME=25:00"),
		append(validEnv(), "REMINDER_COOLDOWN=0s"),
	} {
		if _, err := LoadFile(filepath.Join(t.TempDir(), "missing"), env); err == nil {
			t.Fatal("expected invalid configuration")
		}
	}
}
