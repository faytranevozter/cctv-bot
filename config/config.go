package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	BotToken              string
	SuperuserIDs          map[int64]bool
	AuthorizedChatIDs     []int64
	DBFile                string
	FFmpegBin             string
	FFmpegTimeoutSec      int
	MaxConcurrentCaptures int
}

func Load() (*Config, error) {
	cfg := &Config{
		SuperuserIDs:          make(map[int64]bool),
		DBFile:                envOr("DB_FILE", "cctv_bot.db"),
		FFmpegBin:             envOr("FFMPEG_BIN", "ffmpeg"),
		FFmpegTimeoutSec:      envOrInt("FFMPEG_TIMEOUT_SEC", 15),
		MaxConcurrentCaptures: envOrInt("MAX_CONCURRENT_CAPTURES", 3),
	}

	cfg.BotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	superusers, err := parseIDSet(os.Getenv("SUPERUSER_IDS"), "SUPERUSER_IDS")
	if err != nil {
		return nil, err
	}
	if len(superusers) == 0 {
		return nil, fmt.Errorf("SUPERUSER_IDS must contain at least one user ID")
	}
	cfg.SuperuserIDs = superusers

	authorized, err := parseIDList(os.Getenv("AUTHORIZED_CHAT_IDS"), "AUTHORIZED_CHAT_IDS")
	if err != nil {
		return nil, err
	}
	cfg.AuthorizedChatIDs = authorized

	return cfg, nil
}

func parseIDSet(raw, field string) (map[int64]bool, error) {
	ids, err := parseIDList(raw, field)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

func parseIDList(raw, field string) ([]int64, error) {
	var ids []int64
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ID in %s: %s", field, s)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
