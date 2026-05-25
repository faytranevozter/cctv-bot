You are an expert Go developer. Build a production-ready Telegram bot in Go that acts as a CCTV monitor by capturing frames from RTMP/RTSP streams using FFmpeg and sending them back to the user.

---

## Project structure

```
cctv-bot/
├── main.go
├── bot/
│   ├── handler.go       # Command and message handlers
│   └── middleware.go    # Auth middleware (allowed chat IDs)
├── camera/
│   ├── capture.go       # FFmpeg frame capture logic
│   └── stream.go        # RTMP/RTSP stream definitions
├── config/
│   └── config.go        # Env-based config loader
├── .env.example
└── Dockerfile
```

---

## Requirements

### 1. Telegram bot setup
- Use the `github.com/go-telegram-bot-api/telegram-bot-api/v5` library.
- Load the bot token from the `TELEGRAM_BOT_TOKEN` environment variable.
- Support long-polling for updates.
- Log every incoming command with timestamp and user info.

### 2. Command: /mataelang
- When a user sends `/mataelang`, the bot must:
  1. Reply immediately with a status message: "👁 Capturing frame, please wait..."
  2. Run FFmpeg as a subprocess to grab a single frame from the configured RTMP/RTSP stream.
  3. FFmpeg command to use (capture one frame as JPEG):
     ```
     ffmpeg -rtsp_transport tcp -i <STREAM_URL> -frames:v 1 -q:v 2 /tmp/snapshot_<unix_ts>.jpg -y
     ```
  4. Send the resulting JPEG back to the same chat as a photo using `SendPhoto`.
  5. If capture fails (non-zero exit, timeout, file not found), reply with a clear error message: "❌ Failed to capture frame: <reason>"
  6. Clean up the temporary file after sending.

### 3. Additional commands
- `/start` — Welcome message explaining available commands.
- `/cameras` — List all configured camera names and their stream URLs (mask credentials if present).
- `/snap <camera_name>` — Capture a frame from a named camera (if multiple cameras are configured).
- `/help` — Show command reference.

### 4. Camera configuration
- Define cameras in the `.env` file:
  ```
  CAMERA_1_NAME=GudangDepan
  CAMERA_1_URL=rtmp://192.168.1.10/live/cam1
  CAMERA_2_NAME=PintuMasuk
  CAMERA_2_URL=rtsp://admin:pass@192.168.1.20:554/stream
  ```
- Parse all `CAMERA_N_NAME` / `CAMERA_N_URL` pairs dynamically.
- Default camera (for `/mataelang`) is camera 1.

### 5. Security
- Allowlist of authorized Telegram chat IDs from env: `ALLOWED_CHAT_IDS=123456,789012`
- Any command from an unauthorized chat must be silently ignored or replied with "🔒 Unauthorized."
- Never expose raw stream URLs with credentials in any bot reply.

### 6. FFmpeg execution
- Use `os/exec` with a configurable timeout (default 15 seconds, from env `FFMPEG_TIMEOUT_SEC`).
- FFmpeg binary path configurable via `FFMPEG_BIN` env var (default: `ffmpeg`).
- Capture stderr output for error reporting.
- Temp files must use unique names (use `os.CreateTemp` or timestamp + random suffix) to avoid race conditions.

### 7. Concurrency
- Handle multiple simultaneous capture requests safely.
- Limit concurrent FFmpeg processes to avoid resource exhaustion (use a semaphore, max from env `MAX_CONCURRENT_CAPTURES`, default 3).

### 8. Graceful shutdown
- Listen for `SIGINT`/`SIGTERM`.
- Cancel in-flight FFmpeg processes using context cancellation before exiting.

### 9. Logging
- Use `log/slog` (Go 1.21+) with structured JSON output.
- Log fields: `timestamp`, `level`, `chat_id`, `username`, `command`, `camera`, `duration_ms`, `error`.

### 10. Docker
- Provide a multi-stage `Dockerfile`:
  - Stage 1: `golang:1.22-alpine` to build the binary.
  - Stage 2: `alpine:latest` with `ffmpeg` installed via apk.
- Expose no ports (bot uses long-polling).

---

## Output expected

Provide the complete, compilable Go source code for all files. Include:
- All `import` blocks (no missing imports).
- A working `.env.example` with all variables documented.
- The full `Dockerfile`.
- Inline code comments explaining non-obvious logic (FFmpeg subprocess lifecycle, semaphore pattern, temp file cleanup).
- A brief README section at the end explaining how to run locally and in Docker.

Do not use any external frameworks beyond the Telegram bot library. Keep dependencies minimal.