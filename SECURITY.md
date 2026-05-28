# Security Policy

## Supported Versions

Security fixes are prioritized for the latest released version of CCTV Bot.

If you are running an older version, upgrade to the latest release before reporting an issue unless the vulnerability is clearly still present in the current codebase.

## Reporting a Vulnerability

Do not report security vulnerabilities in public issues, pull requests, or discussions.

Email vulnerability reports to:

```text
mfahrurrifai@gmail.com
```

Please include:

- Affected version, commit, or Docker image tag.
- Clear reproduction steps or a minimal proof of concept.
- Expected and actual behavior.
- Impact assessment, including whether the issue can expose cameras, credentials, chat data, or bot control.
- Relevant logs with secrets removed.

Do not include real production secrets. Redact or replace:

- `TELEGRAM_BOT_TOKEN` values.
- Real RTSP, RTMP, or HLS stream credentials.
- `.env` files.
- SQLite database files.
- Chat IDs, user IDs, or usernames when they are not necessary for reproduction.
- Production logs containing camera URLs, tokens, credentials, or private operational details.

## Scope

Examples of in-scope security issues include:

- Authorization bypasses that allow unauthorized chats or users to access camera snapshots.
- Superuser privilege bypasses.
- Telegram bot token leakage.
- Camera stream credential leakage in logs, messages, errors, or Docker output.
- Unsafe handling of camera URLs or FFmpeg invocation.
- Docker image or GitHub Actions behavior that exposes secrets or weakens release integrity.

Examples of out-of-scope reports include:

- Vulnerabilities that require already having full host access.
- Issues caused only by publishing your own `.env`, database, camera URLs, or bot token.
- Generic dependency reports without a reachable impact in this project.

## Response Process

Reports will be acknowledged as soon as practical. The maintainer may ask for clarification, a safer reproduction, or additional impact details.

When a vulnerability is confirmed, the maintainer will coordinate a fix and release. Please allow time for a patch before public disclosure.

## Operational Security Notes

- Use a dedicated Telegram bot token for each deployment.
- Keep `.env`, SQLite databases, and camera stream credentials out of version control.
- Treat Docker image environment variables and mounted `/data` contents as sensitive.
- Rotate the Telegram bot token and camera credentials if they are exposed.
