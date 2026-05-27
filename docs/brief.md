You are an expert Go developer. Build a production-ready Telegram bot in Go that acts as a CCTV monitor by capturing frames from RTMP/RTSP streams using FFmpeg and sending them back to the user.

---

## Project structure

```
cctv-bot/
├── main.go
├── bot/
│   └── bot.go           # Command handlers + auth middleware
├── camera/
│   ├── capture.go       # FFmpeg frame capture + concurrency semaphore
│   ├── store.go         # JSON-file-backed camera list (atomic, RWMutex)
│   └── stream.go        # URL credential masking
├── config/
│   └── config.go        # Env-based config loader
├── .env.example
├── cameras.json         # Camera list (created at runtime, not committed)
└── Dockerfile
```

---

## Requirements

### 1. Telegram bot setup
- Use the `github.com/go-telegram/bot` library.
- Load the bot token from the `TELEGRAM_BOT_TOKEN` environment variable.
- Use long-polling for updates.
- Log every incoming command with timestamp and user info.

### 2. Command: /mataelang
- When a user sends `/mataelang`, the bot must:
  1. Reply immediately with a status message: "Capturing frame, please wait..."
  2. Run FFmpeg as a subprocess to grab a single frame from the default camera.
  3. FFmpeg command:
     ```
     ffmpeg -rtsp_transport tcp -i <STREAM_URL> -frames:v 1 -q:v 2 <tmpfile>.jpg -y
     ```
  4. Send the JPEG back to the chat with `SendPhoto`.
  5. On failure (non-zero exit, timeout, empty file) reply with `Failed to capture frame: <reason>`.
  6. Clean up the temp file after sending.

### 3. Additional commands
- `/start` — Welcome message with command reference.
- `/help` — Same as `/start`.
- `/cameras` — List configured cameras with credentials masked.
- `/snap <camera_name>` — Capture from a named camera.
- `/addcam "<name>" <url>` — Add a camera. Quote the name when it contains spaces.
- `/delcam <name>` — Remove a camera by name.

### 4. Camera storage (no database server)
- Cameras are persisted in a JSON file at `${CAMERAS_FILE}` (default `./cameras.json`, `/data/cameras.json` in Docker).
- File format: a JSON array of `{"name": string, "url": string}` objects.
- The first entry is the default camera used by `/mataelang`.
- The store is concurrency-safe (`sync.RWMutex`) and writes are atomic (temp file + `os.Rename`).
- The file is created lazily on the first write; a missing or empty file is treated as an empty list.
- Lookups (`/snap`, `/delcam`) match camera names case-insensitively.

### 5. Security
- Allowlist of authorized Telegram chat IDs from env: `ALLOWED_CHAT_IDS=123456,789012`.
- Commands from unauthorized chats are silently dropped.
- Stream URLs containing `user:password@` are masked to `***:***@` in any bot reply or log.

### 6. FFmpeg execution
- Use `os/exec.CommandContext` with a timeout from `FFMPEG_TIMEOUT_SEC` (default 15).
- Binary path from `FFMPEG_BIN` (default `ffmpeg`).
- Capture combined stdout/stderr for error reporting.
- Temp files via `os.CreateTemp` so concurrent captures don't collide.

### 7. Concurrency
- Multiple capture requests run concurrently up to `MAX_CONCURRENT_CAPTURES` (default 3) using a buffered-channel semaphore.

### 8. Graceful shutdown
- `signal.NotifyContext` on `SIGINT`/`SIGTERM` cancels the bot context, which propagates into in-flight FFmpeg subprocesses via `CommandContext`.

### 9. Logging
- `log/slog` with JSON output.
- Fields used: `timestamp`, `level`, `chat_id`, `username`, `command`, `camera`, `duration_ms`, `error`.

### 10. Docker
- Multi-stage `Dockerfile`:
  - Stage 1: `golang:1.26-alpine` builds a static binary (`CGO_ENABLED=0`).
  - Stage 2: `alpine:3.21` with `ffmpeg`, runs as non-root user `cctv`.
- `/data` is declared as a `VOLUME` and `CAMERAS_FILE=/data/cameras.json` so the camera list survives container restarts.
- No ports exposed (long-polling).

---

## Running

### Local
```sh
cp .env.example .env       # fill in TELEGRAM_BOT_TOKEN and ALLOWED_CHAT_IDS
make run
```
Add cameras from Telegram with `/addcam`, or hand-edit `cameras.json`.

### Docker
```sh
make docker-build
make docker-run            # mounts ./data into /data inside the container
```
The host directory `./data/` will hold `cameras.json` across container runs.
