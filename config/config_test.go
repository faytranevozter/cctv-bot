package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresBotToken(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("SUPERUSER_IDS", "123")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN is required") {
		t.Fatalf("Load() error = %v, want TELEGRAM_BOT_TOKEN required", err)
	}
}

func TestLoadRequiresSuperusers(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("SUPERUSER_IDS", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "SUPERUSER_IDS must contain at least one user ID") {
		t.Fatalf("Load() error = %v, want SUPERUSER_IDS required", err)
	}
}

func TestLoadRejectsMalformedIDs(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("SUPERUSER_IDS", "123,nope")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "invalid ID in SUPERUSER_IDS") {
		t.Fatalf("Load() error = %v, want invalid ID", err)
	}
}

func TestLoadRejectsInvalidTimezone(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("SUPERUSER_IDS", "123")
	t.Setenv("TIMEZONE", "Invalid/Zone")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "invalid TIMEZONE") {
		t.Fatalf("Load() error = %v, want invalid TIMEZONE", err)
	}
}

func TestLoadUsesDefaultsAndParsesIDs(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("SUPERUSER_IDS", "123, 456")
	t.Setenv("AUTHORIZED_CHAT_IDS", "789, -100")
	t.Setenv("DB_FILE", "")
	t.Setenv("FFMPEG_BIN", "")
	t.Setenv("FFMPEG_TIMEOUT_SEC", "")
	t.Setenv("MAX_CONCURRENT_CAPTURES", "")
	t.Setenv("TIMEZONE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BotToken != "token" {
		t.Fatalf("BotToken = %q, want token", cfg.BotToken)
	}
	if !cfg.SuperuserIDs[123] || !cfg.SuperuserIDs[456] || len(cfg.SuperuserIDs) != 2 {
		t.Fatalf("SuperuserIDs = %#v, want 123 and 456", cfg.SuperuserIDs)
	}
	if got, want := cfg.AuthorizedChatIDs, []int64{789, -100}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("AuthorizedChatIDs = %#v, want %#v", got, want)
	}
	if cfg.DBFile != "cctv_bot.db" {
		t.Fatalf("DBFile = %q, want default", cfg.DBFile)
	}
	if cfg.FFmpegBin != "ffmpeg" {
		t.Fatalf("FFmpegBin = %q, want default", cfg.FFmpegBin)
	}
	if cfg.FFmpegTimeoutSec != 15 {
		t.Fatalf("FFmpegTimeoutSec = %d, want 15", cfg.FFmpegTimeoutSec)
	}
	if cfg.MaxConcurrentCaptures != 3 {
		t.Fatalf("MaxConcurrentCaptures = %d, want 3", cfg.MaxConcurrentCaptures)
	}
	if cfg.Timezone != "Asia/Jakarta" || cfg.Location == nil {
		t.Fatalf("Timezone = %q, Location = %v, want Asia/Jakarta location", cfg.Timezone, cfg.Location)
	}
}

func TestLoadUsesConfiguredValues(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("SUPERUSER_IDS", "123")
	t.Setenv("DB_FILE", "custom.db")
	t.Setenv("FFMPEG_BIN", "/bin/ffmpeg")
	t.Setenv("FFMPEG_TIMEOUT_SEC", "30")
	t.Setenv("MAX_CONCURRENT_CAPTURES", "7")
	t.Setenv("TIMEZONE", "UTC")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DBFile != "custom.db" || cfg.FFmpegBin != "/bin/ffmpeg" || cfg.FFmpegTimeoutSec != 30 || cfg.MaxConcurrentCaptures != 7 || cfg.Timezone != "UTC" {
		t.Fatalf("Load() = %#v, want configured values", cfg)
	}
}
