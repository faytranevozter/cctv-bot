package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type Camera struct {
	Name string
	URL  string
}

type Config struct {
	BotToken            string
	AllowedChatIDs      map[int64]bool
	Cameras             []Camera
	FFmpegBin           string
	FFmpegTimeoutSec    int
	MaxConcurrentCaptures int
}

func Load() (*Config, error) {
	cfg := &Config{
		AllowedChatIDs:        make(map[int64]bool),
		FFmpegBin:             envOr("FFMPEG_BIN", "ffmpeg"),
		FFmpegTimeoutSec:      envOrInt("FFMPEG_TIMEOUT_SEC", 15),
		MaxConcurrentCaptures: envOrInt("MAX_CONCURRENT_CAPTURES", 3),
	}

	cfg.BotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	raw := os.Getenv("ALLOWED_CHAT_IDS")
	if raw == "" {
		return nil, fmt.Errorf("ALLOWED_CHAT_IDS is required")
	}
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid chat ID in ALLOWED_CHAT_IDS: %s", s)
		}
		cfg.AllowedChatIDs[id] = true
	}
	if len(cfg.AllowedChatIDs) == 0 {
		return nil, fmt.Errorf("ALLOWED_CHAT_IDS must contain at least one chat ID")
	}

	cfg.Cameras = parseCameras()
	if len(cfg.Cameras) == 0 {
		return nil, fmt.Errorf("at least one CAMERA_1_NAME/CAMERA_1_URL pair is required")
	}

	return cfg, nil
}

func (c *Config) DefaultCamera() Camera {
	return c.Cameras[0]
}

func (c *Config) FindCamera(name string) (Camera, bool) {
	for _, cam := range c.Cameras {
		if strings.EqualFold(cam.Name, name) {
			return cam, true
		}
	}
	return Camera{}, false
}

func parseCameras() []Camera {
	var cameras []Camera
	type kv struct{ k, v string }
	pairs := make(map[int]kv)

	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		key, val := parts[0], parts[1]

		if strings.HasPrefix(key, "CAMERA_") && strings.HasSuffix(key, "_NAME") {
			nStr := strings.TrimPrefix(key, "CAMERA_")
			nStr = strings.TrimSuffix(nStr, "_NAME")
			if n, err := strconv.Atoi(nStr); err == nil {
				p := pairs[n]
				p.k = val
				pairs[n] = p
			}
		}
		if strings.HasPrefix(key, "CAMERA_") && strings.HasSuffix(key, "_URL") {
			nStr := strings.TrimPrefix(key, "CAMERA_")
			nStr = strings.TrimSuffix(nStr, "_URL")
			if n, err := strconv.Atoi(nStr); err == nil {
				p := pairs[n]
				p.v = val
				pairs[n] = p
			}
		}
	}

	indices := make([]int, 0, len(pairs))
	for n := range pairs {
		indices = append(indices, n)
	}
	sort.Ints(indices)

	for _, n := range indices {
		p := pairs[n]
		if p.k != "" && p.v != "" {
			cameras = append(cameras, Camera{Name: p.k, URL: p.v})
		}
	}

	return cameras
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
