# CCTV Bot

CCTV Bot is a Telegram bot that captures single JPEG frames from RTSP or RTMP camera streams with FFmpeg and sends them back to authorized Telegram chats.

## Features

- Capture a frame from the default camera.
- Capture a frame from a named camera.
- Add, remove, and list cameras from Telegram chat.
- Persist camera configuration in a JSON file.
- Automatically register Telegram commands on startup so users can see them from the chat command menu.
- Restrict access by Telegram chat ID allowlist.
- Mask camera stream credentials in replies and logs where URLs are displayed.
- Limit concurrent captures to protect the host and cameras.
- Run locally or in Docker with FFmpeg included in the image.

## Commands

The bot registers these commands with Telegram on startup:

| Command | Description |
| --- | --- |
| `/mataelang` | Capture from the default camera. |
| `/snap <name>` | Capture from a specific camera. |
| `/cameras` | List configured cameras. |
| `/addcam "<name>" <url>` | Add a camera. Quote names that contain spaces. |
| `/delcam <name>` | Remove a camera. |
| `/help` | Show the command reference. |

Examples:

```text
/addcam "Front Gate" rtsp://user:pass@192.168.1.10/stream
/cameras
/snap "Front Gate"
/delcam "Front Gate"
```

## Requirements

- Go 1.26 or newer for local builds.
- FFmpeg available on `PATH`, unless `FFMPEG_BIN` points to another binary.
- A Telegram bot token from BotFather.
- One or more authorized Telegram chat IDs.

The Docker image installs FFmpeg automatically.

## Configuration

Create a local environment file from the example:

```sh
make env
```

Then edit `.env` and set the required values.

Required variables:

| Variable | Description |
| --- | --- |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token from BotFather. |
| `ALLOWED_CHAT_IDS` | Comma-separated Telegram chat IDs allowed to use the bot. |

Optional variables:

| Variable | Default | Description |
| --- | --- | --- |
| `CAMERAS_FILE` | `cameras.json` | Path to the JSON camera store. Docker sets this to `/data/cameras.json`. |
| `FFMPEG_BIN` | `ffmpeg` | FFmpeg executable path. |
| `FFMPEG_TIMEOUT_SEC` | `15` | Capture timeout in seconds. |
| `MAX_CONCURRENT_CAPTURES` | `3` | Maximum capture jobs running at the same time. |

Example:

```env
TELEGRAM_BOT_TOKEN=123456:ABC-DEF1234gh
ALLOWED_CHAT_IDS=123456789,987654321
CAMERAS_FILE=cameras.json
FFMPEG_BIN=ffmpeg
FFMPEG_TIMEOUT_SEC=15
MAX_CONCURRENT_CAPTURES=3
```

## Camera Storage

Cameras are stored in a JSON file. The first entry is the default camera used by `/mataelang`.

Example `cameras.json`:

```json
[
  {
    "name": "Front Gate",
    "url": "rtsp://user:pass@192.168.1.10/stream"
  }
]
```

The store is safe for concurrent reads and writes. Updates are written atomically with a temporary file and rename.

If the JSON file is empty and legacy `CAMERA_N_NAME` / `CAMERA_N_URL` variables are present, the bot migrates them into the JSON file once on startup. After that, the JSON file is the source of truth.

## Local Development

Install dependencies and run the bot:

```sh
make env
make run
```

Common development commands:

```sh
make fmt
make vet
make test
make build
```

`make test` runs:

```sh
go test ./... -race -count=1
```

## Docker

Build the image:

```sh
make docker-build
```

Run the image with `.env` and a persistent `./data` directory:

```sh
make docker-run
```

The Docker image sets:

```env
CAMERAS_FILE=/data/cameras.json
```

The `./data` directory on the host is mounted to `/data` in the container so camera configuration survives container restarts.

## Security Notes

- Only chat IDs in `ALLOWED_CHAT_IDS` can use the bot.
- Unauthorized chats are ignored.
- Do not commit `.env`, real bot tokens, or private camera stream URLs.
- Camera stream credentials are masked in bot replies and logs where URLs are displayed.
- The bot uses long polling and does not expose an HTTP port.

## Troubleshooting

### `TELEGRAM_BOT_TOKEN is required`

Set `TELEGRAM_BOT_TOKEN` in `.env` or in the process environment.

### `ALLOWED_CHAT_IDS is required`

Set `ALLOWED_CHAT_IDS` to one or more comma-separated Telegram chat IDs.

### Commands do not appear in Telegram

The bot registers commands on startup with Telegram. Restart the bot and check logs for `bot command registration failed`. Telegram clients can also take a short time to refresh the command menu.

### No cameras are configured

Add one from Telegram:

```text
/addcam "Front Gate" rtsp://user:pass@192.168.1.10/stream
```

Or edit `cameras.json` directly.

### FFmpeg is not found

Install FFmpeg or set `FFMPEG_BIN` to the full binary path.

### Capture times out

Check that the RTSP/RTMP URL is reachable from the bot host. If the camera is slow, increase `FFMPEG_TIMEOUT_SEC`.

### Unauthorized chat cannot use the bot

Add the chat ID to `ALLOWED_CHAT_IDS` and restart the bot.

## Project Structure

```text
cctv-bot/
├── main.go              # Application startup and Telegram bot initialization
├── bot/
│   └── bot.go           # Command handlers, command registration data, auth middleware
├── camera/
│   ├── capture.go       # FFmpeg frame capture
│   ├── store.go         # JSON-backed camera store
│   └── stream.go        # Camera URL credential masking
├── config/
│   └── config.go        # Environment-based configuration loader
├── docs/
│   └── brief.md         # Original implementation brief
├── .env.example         # Example environment configuration
├── cameras.json         # Default local camera store
├── Dockerfile           # Multi-stage Docker build with FFmpeg runtime
├── Makefile             # Local development and Docker commands
└── README.md
```
