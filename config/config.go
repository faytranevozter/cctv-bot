package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/faytranevozter/cctv-bot/camera"
)

type Config struct {
	BotToken              string
	SuperuserIDs          map[int64]bool
	AuthorizedChatIDs     []int64
	AuthFile              string
	CamerasFile           string
	LegacyCameras         []camera.Camera // parsed from CAMERA_N_NAME/_URL env vars, used only for one-shot migration
	FFmpegBin             string
	FFmpegTimeoutSec      int
	MaxConcurrentCaptures int
}

func Load() (*Config, error) {
	cfg := &Config{
		SuperuserIDs:          make(map[int64]bool),
		AuthFile:              envOr("AUTH_FILE", "authorized_chats.json"),
		CamerasFile:           envOr("CAMERAS_FILE", "cameras.json"),
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

	cfg.LegacyCameras = parseLegacyCameras()

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

// parseLegacyCameras reads CAMERA_N_NAME / CAMERA_N_URL pairs from the
// environment. These are no longer the authoritative source; they are kept
// only to migrate existing deployments into the JSON store on first run.
func parseLegacyCameras() []camera.Camera {
	type kv struct{ name, url string }
	pairs := make(map[int]kv)

	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]

		if !strings.HasPrefix(key, "CAMERA_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_NAME"):
			nStr := strings.TrimSuffix(strings.TrimPrefix(key, "CAMERA_"), "_NAME")
			if n, err := strconv.Atoi(nStr); err == nil {
				p := pairs[n]
				p.name = val
				pairs[n] = p
			}
		case strings.HasSuffix(key, "_URL"):
			nStr := strings.TrimSuffix(strings.TrimPrefix(key, "CAMERA_"), "_URL")
			if n, err := strconv.Atoi(nStr); err == nil {
				p := pairs[n]
				p.url = val
				pairs[n] = p
			}
		}
	}

	indices := make([]int, 0, len(pairs))
	for n := range pairs {
		indices = append(indices, n)
	}
	sort.Ints(indices)

	cams := make([]camera.Camera, 0, len(indices))
	for _, n := range indices {
		p := pairs[n]
		if p.name != "" && p.url != "" {
			cams = append(cams, camera.Camera{Name: p.name, URL: p.url})
		}
	}
	return cams
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
