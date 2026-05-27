# CCTV Bot

CCTV Bot is a Telegram bot that captures single JPEG frames from RTSP or RTMP camera streams with FFmpeg and sends them back to authorized Telegram chats.

## Features

- Capture a frame from a named camera.
- Capture a camera directly from a managed shortcut command such as `/gamping`.
- Add, remove, and list cameras from Telegram chat.
- Persist camera configuration in a JSON file.
- Automatically create camera shortcuts when adding cameras when the generated shortcut is valid and available.
- Automatically register built-in commands and camera shortcuts on startup so users can see them from the chat command menu.
- Restrict access through superuser-approved chat authorization requests.
- Mask camera stream credentials in replies and logs where URLs are displayed.
- Limit concurrent captures to protect the host and cameras.
- Run locally or in Docker with FFmpeg included in the image.

## Commands

The bot registers these commands with Telegram on startup:

| Command | Description |
| --- | --- |
| `/requestaccess [reason]` | Request access for the current chat. |
| `/authorized` | Superuser dashboard for pending requests and authorized chats. |
| `/snap <name>` | Capture from a specific camera. |
| `/cameras` | List configured cameras. |
| `/addcam "<name>" <url>` | Add a camera. Quote names that contain spaces. |
| `/delcam <name>` | Remove a camera. |
| `/setshortcut "<name>" <shortcut>` | Assign or replace a camera shortcut. |
| `/delshortcut <name>` | Remove a camera shortcut. |
| `/help` | Show the command reference. |

Management commands are admin-only in groups and supergroups:

```text
/addcam
/delcam
/setshortcut
/delshortcut
```

In private chats, these commands are allowed when the private chat is authorized or the sender is a superuser.

Camera shortcuts are also registered as commands. For example, if camera `Gamping` has shortcut `gamping`, users can run:

```text
/gamping
```

In groups with multiple bots, commands can include this bot's username:

```text
/cameras@cctipsibot
/gamping@cctipsibot
```

Commands targeted at another bot username are ignored.

Examples:

```text
/addcam "Front Gate" rtsp://user:pass@192.168.1.10/stream
/cameras
/snap "Front Gate"
/setshortcut "Front Gate" front_gate
/front_gate
/delshortcut "Front Gate"
/delcam "Front Gate"
```

## Requirements

- Go 1.26 or newer for local builds.
- FFmpeg available on `PATH`, unless `FFMPEG_BIN` points to another binary.
- A Telegram bot token from BotFather.
- One or more Telegram superuser IDs.

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
| `SUPERUSER_IDS` | Comma-separated Telegram user IDs allowed to approve/reject access requests and revoke access. |

Optional variables:

| Variable | Default | Description |
| --- | --- | --- |
| `AUTHORIZED_CHAT_IDS` | empty | Optional bootstrap list of pre-authorized chat IDs. Runtime approvals are stored in `AUTH_FILE`. |
| `AUTH_FILE` | `authorized_chats.json` | Path to the JSON authorization store. |
| `CAMERAS_FILE` | `cameras.json` | Path to the JSON camera store. Docker sets this to `/data/cameras.json`. |
| `FFMPEG_BIN` | `ffmpeg` | FFmpeg executable path. |
| `FFMPEG_TIMEOUT_SEC` | `15` | Capture timeout in seconds. |
| `MAX_CONCURRENT_CAPTURES` | `3` | Maximum capture jobs running at the same time. |

Example:

```env
TELEGRAM_BOT_TOKEN=123456:ABC-DEF1234gh
SUPERUSER_IDS=123456789
AUTHORIZED_CHAT_IDS=123456789,-1001234567890
AUTH_FILE=authorized_chats.json
CAMERAS_FILE=cameras.json
FFMPEG_BIN=ffmpeg
FFMPEG_TIMEOUT_SEC=15
MAX_CONCURRENT_CAPTURES=3
```

## Authorization

The bot no longer uses `ALLOWED_CHAT_IDS`. Access is managed by superusers configured in `SUPERUSER_IDS`.

Unauthorized chats can request access:

```text
/requestaccess Need CCTV access for this group
```

For groups and supergroups, only a group owner/admin can request access. Private chats can request access directly.

When a request is created, each superuser receives a private message with inline buttons:

```text
[Approve] [Reject]
```

Approving a request authorizes the chat and stores it in `AUTH_FILE`. Rejecting removes the pending request.

Superusers can manage access from their private chat:

```text
/authorized
```

The dashboard shows both authorized chats and pending requests. Authorized chats have a manage button that opens a revoke screen. Pending requests have approve/reject buttons.

## Camera Storage

Cameras are stored in a JSON file. There is no default camera command; capture by camera name with `/snap <name>` or by a configured shortcut such as `/gamping`.

When a camera is added with `/addcam`, the bot tries to create a shortcut automatically from the camera name:

| Camera Name | Auto Shortcut |
| --- | --- |
| `Gamping` | `/gamping` |
| `Front Gate` | `/front_gate` |
| `Kantor-Kiri` | `/kantor_kiri` |
| `CAM 01` | `/cam_01` |

Shortcuts must be 1-32 characters and contain only lowercase letters, numbers, and underscores. Built-in commands such as `/help`, `/snap`, and `/cameras` are reserved and cannot be used as camera shortcuts.

Example `cameras.json`:

```json
[
  {
    "name": "Front Gate",
    "shortcut": "front_gate",
    "url": "rtsp://user:pass@192.168.1.10/stream"
  }
]
```

The store is safe for concurrent reads and writes. Updates are written atomically with a temporary file and rename.

The `shortcut` field is optional. Existing camera entries without shortcuts still work with `/snap <name>` and can be assigned a shortcut later:

```text
/setshortcut "Front Gate" front_gate
```

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

- Only superusers in `SUPERUSER_IDS` can approve/reject access requests or revoke chat access.
- Unauthorized chats can only use `/start`, `/help`, and `/requestaccess`.
- In groups and supergroups, only Telegram admins can add/remove cameras or manage shortcuts.
- Do not commit `.env`, real bot tokens, or private camera stream URLs.
- Camera stream credentials are masked in bot replies and logs where URLs are displayed.
- The bot uses long polling and does not expose an HTTP port.

## Troubleshooting

### `TELEGRAM_BOT_TOKEN is required`

Set `TELEGRAM_BOT_TOKEN` in `.env` or in the process environment.

### `SUPERUSER_IDS must contain at least one user ID`

Set `SUPERUSER_IDS` to one or more comma-separated Telegram user IDs.

### This chat is not authorized

Ask a group admin to request access:

```text
/requestaccess Need access for CCTV monitoring
```

A superuser must approve the request using the inline button sent to their private chat.

### Commands or camera shortcuts do not appear in Telegram

The bot registers commands on startup and after camera shortcut changes. Restart the bot and check logs for `bot command registration failed`. Telegram clients can also take a short time to refresh the command menu.

### No cameras are configured

Add one from Telegram:

```text
/addcam "Front Gate" rtsp://user:pass@192.168.1.10/stream
```

Or edit `cameras.json` directly.

### A shortcut was not created automatically

The generated shortcut may be invalid, reserved, or already used. Set one manually:

```text
/setshortcut "Front Gate" front_gate
```

### FFmpeg is not found

Install FFmpeg or set `FFMPEG_BIN` to the full binary path.

### Capture times out

Check that the RTSP/RTMP URL is reachable from the bot host. If the camera is slow, increase `FFMPEG_TIMEOUT_SEC`.

## Project Structure

```text
cctv-bot/
├── main.go              # Application startup and Telegram bot initialization
├── bot/
│   └── bot.go           # Command handlers, command registration data, access checks
├── auth/
│   └── store.go         # JSON-backed authorization store
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
