# Contributing

Thanks for your interest in contributing to CCTV Bot.

Repository URL:

```text
https://github.com/faytranevozter/cctv-bot
```

CCTV Bot is a Go Telegram bot that captures JPEG snapshots from FFmpeg-supported camera streams and sends them to authorized Telegram chats.

## Before You Start

- Open an issue first for larger changes, behavior changes, or security-sensitive work.
- Keep pull requests focused and easy to review.
- Update documentation when changing commands, configuration, Docker behavior, release behavior, or security assumptions.
- Do not include real Telegram bot tokens, camera URLs, credentials, `.env` files, SQLite databases, or production logs.

## Local Development

Requirements:

- Go 1.26 or newer.
- FFmpeg available on `PATH`, unless `FFMPEG_BIN` points to another binary.
- A Telegram bot token for manual testing.
- One or more Telegram superuser IDs.

Create a local environment file:

```sh
make env
```

Then edit `.env` with local development values.

Run the bot:

```sh
make run
```

## Common Commands

Format code:

```sh
make fmt
```

Run `go vet`:

```sh
make vet
```

Run tests:

```sh
make test
```

Build a local binary:

```sh
make build
```

Build a local Docker image:

```sh
make docker-build
```

Run the local Docker image:

```sh
make docker-run
```

## Development Guidelines

- Preserve authorization checks for chats, superusers, and camera management.
- Keep camera management restricted to superuser private chat unless there is a deliberate security review.
- Preserve credential masking in replies and logs.
- Avoid logging full camera URLs, Telegram tokens, or user-provided secrets.
- Keep database migrations compatible with existing SQLite deployments.
- Keep Docker image changes compatible with the `/data` volume and `DB_FILE=/data/cctv_bot.db` behavior.
- Prefer small, direct changes over broad rewrites.

## Pull Request Checklist

Before opening a pull request, verify:

- `make fmt` passes.
- `make vet` passes.
- `make test` passes.
- Documentation is updated if commands, configuration, Docker behavior, authorization behavior, or release behavior changed.
- No secrets, private camera URLs, `.env` files, SQLite databases, or production logs are included.
- Security-sensitive behavior is explained in the pull request description.

## Reporting Security Issues

Do not open public issues for vulnerabilities.

Email security reports to:

```text
mfahrurrifai@gmail.com
```

See `SECURITY.md` for the full policy.

## Code of Conduct

By participating in this project, you agree to follow `CODE_OF_CONDUCT.md`.
