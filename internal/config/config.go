package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	ApplicationTimezone     = "Asia/Ho_Chi_Minh"
	DefaultReminderTime     = "20:30"
	DefaultReminderCooldown = 24 * time.Hour
)

var requiredKeys = []string{
	"DISCORD_TOKEN",
	"DISCORD_GUILD_ID",
	"DISCORD_USER_ID",
	"DISCORD_CHANNEL_ID",
	"OPENAI_BASE_URL",
	"OPENAI_MODEL",
	"SQLITE_PATH",
}

// Config contains validated startup settings. Location is always the fixed
// application timezone, independent of the host timezone.
type Config struct {
	DiscordToken     string
	DiscordGuildID   string
	DiscordUserID    string
	DiscordChannelID string
	OpenAIBaseURL    string
	OpenAIAPIKey     string
	OpenAIModel      string
	SQLitePath       string

	ReminderEnabled  bool
	ReminderTime     string
	ReminderCooldown time.Duration
	Location         *time.Location
}

// Load reads .env when present, then lets process environment variables win.
func Load() (Config, error) {
	return LoadFile(".env", os.Environ())
}

// LoadFile reads a dotenv file when present and validates the resulting values.
// Missing dotenv files are allowed; missing required settings are not.
func LoadFile(path string, environ []string) (Config, error) {
	values := make(map[string]string)
	if data, err := os.ReadFile(path); err == nil {
		parsed, parseErr := parseDotEnv(string(data))
		if parseErr != nil {
			return Config{}, fmt.Errorf("parse %s: %w", path, parseErr)
		}
		for key, value := range parsed {
			values[key] = value
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	for _, item := range environ {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	return fromValues(values)
}

func fromValues(values map[string]string) (Config, error) {
	for _, key := range requiredKeys {
		if strings.TrimSpace(values[key]) == "" {
			return Config{}, fmt.Errorf("missing required configuration %s", key)
		}
	}
	baseURL, err := validateBaseURL(values["OPENAI_BASE_URL"])
	if err != nil {
		return Config{}, fmt.Errorf("OPENAI_BASE_URL: %w", err)
	}
	if err := validateSQLitePath(values["SQLITE_PATH"]); err != nil {
		return Config{}, fmt.Errorf("SQLITE_PATH: %w", err)
	}
	reminderEnabled, err := parseBool(values["REMINDER_ENABLED"], false)
	if err != nil {
		return Config{}, fmt.Errorf("REMINDER_ENABLED: %w", err)
	}
	reminderTime := strings.TrimSpace(values["REMINDER_TIME"])
	if reminderTime == "" {
		reminderTime = DefaultReminderTime
	}
	if _, err := time.Parse("15:04", reminderTime); err != nil {
		return Config{}, fmt.Errorf("REMINDER_TIME: expected HH:MM: %w", err)
	}
	cooldown := DefaultReminderCooldown
	if raw := strings.TrimSpace(values["REMINDER_COOLDOWN"]); raw != "" {
		cooldown, err = time.ParseDuration(raw)
		if err != nil || cooldown <= 0 {
			return Config{}, fmt.Errorf("REMINDER_COOLDOWN: expected a positive duration")
		}
	}
	location, err := time.LoadLocation(ApplicationTimezone)
	if err != nil {
		return Config{}, fmt.Errorf("load application timezone: %w", err)
	}
	return Config{
		DiscordToken: values["DISCORD_TOKEN"], DiscordGuildID: values["DISCORD_GUILD_ID"],
		DiscordUserID: values["DISCORD_USER_ID"], DiscordChannelID: values["DISCORD_CHANNEL_ID"],
		OpenAIBaseURL: baseURL, OpenAIAPIKey: values["OPENAI_API_KEY"], OpenAIModel: values["OPENAI_MODEL"],
		SQLitePath: values["SQLITE_PATH"], ReminderEnabled: reminderEnabled, ReminderTime: reminderTime,
		ReminderCooldown: cooldown, Location: location,
	}, nil
}

func validateSQLitePath(raw string) error {
	path := strings.TrimSpace(raw)
	if path == "" {
		return errors.New("path is required")
	}
	if path != ":memory:" && strings.IndexByte(path, 0) >= 0 {
		return errors.New("path contains a NUL byte")
	}
	if path != ":memory:" && filepath.Clean(path) == "." {
		return errors.New("path must name a database file")
	}
	return nil
}

func validateBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("must be an absolute http or https URL")
	}
	if u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(u.Path, "/responses") {
		return "", fmt.Errorf("must be an API root without query, fragment, or /responses path")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func parseBool(raw string, fallback bool) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	return strconv.ParseBool(strings.TrimSpace(raw))
}

func parseDotEnv(data string) (map[string]string, error) {
	values := make(map[string]string)
	for lineNumber, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", lineNumber+1)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	return values, nil
}
