<p align="center">
  <img src="assets/qorvexus-logo.svg" alt="Qorvexus logo" width="720">
</p>

# Qorvexus

[![CI](https://github.com/fwerkor/qorvexus/actions/workflows/ci.yml/badge.svg)](https://github.com/fwerkor/qorvexus/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/fwerkor/qorvexus)](https://github.com/fwerkor/qorvexus/releases)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE)

Qorvexus is a maintainable Go agent platform for building an always-on assistant with tools, memory, social channels, self-improvement workflows, and a built-in control panel.

Maintained by `FWERKOR Team`  
Contact: `admin@fwerkor.com`

> Think of it as an agent runtime, not a single hard-coded bot.
> Models, tools, skills, sessions, background jobs, and social channels are all explicit subsystems.

## What It Already Does

| Area | Current baseline |
| --- | --- |
| Models | OpenAI-compatible adapters, multimodal message parts, vision fallback, model-call recording |
| Agent loop | Tool calling, sub-agents, multi-model consultation, scheduled tasks |
| Skills | OpenClaw-style `SKILL.md` loading with frontmatter gating |
| Runtime | Sessions, context compression, queue worker, cron scheduling, audit log |
| Memory | Durable notes plus recall tools |
| Control plane | Built-in Web UI for config editing, inspection, and operations |
| Social | Owner-aware trust boundaries plus plugin-based social channels |
| Self-improvement | Config editing, skill writing, backlog capture, promotion into tasks |

## Why The Project Is Shaped This Way

- `model` is isolated so new providers or logging shims can be added without touching the core loop.
- `tool` is first-class and model-visible, so the model decides when to act.
- `skill` stays compatible with OpenClaw-style `SKILL.md` workflows.
- `session`, `memory`, `scheduler`, and `taskqueue` are explicit subsystems instead of hidden prompt behavior.
- `socialplugin` keeps channels like Telegram and Discord optional, so adding Slack later follows the same boundary.
- `self` and `audit` make self-modification possible without making it invisible.

## Quick Start

1. Build the binary:

```bash
go build ./cmd/qorvexus
```

2. Start Qorvexus once to generate `qorvexus.yaml`:

```bash
./qorvexus start
```

3. Stop it after the file appears, then add your model and channel credentials:

```yaml
models:
  primary:
    provider: openai-compatible
    base_url: https://api.openai.com/v1
    api_key: "your-model-key"
    model: gpt-4.1

social:
  allowed_channels:
    - telegram
    - discord
    - slack
  telegram:
    bot_token: "your-telegram-bot-token"
  discord:
    bot_token: "your-discord-bot-token"
    default_channel_id: "your-channel-id"
  slack:
    bot_token: "your-slack-bot-token"
    default_channel_id: "your-channel-id"
```

4. Start the full service:

```bash
./qorvexus start
```

5. Open the control panel:

```text
http://127.0.0.1:7788
```

Qorvexus fills in many internal defaults automatically, but model connection settings remain explicit because they are part of the deployment contract.

## Community

- License: [GPL v3](LICENSE)
- Security: [SECURITY.md](SECURITY.md)
- Contributing: [CONTRIBUTING.md](CONTRIBUTING.md)
- Support: [SUPPORT.md](SUPPORT.md)
- Conduct: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)

## Common Commands

```bash
./qorvexus start
./qorvexus run "Plan my day and execute any necessary research"
./qorvexus run --image https://example.com/screen.png "Describe this screen"
./qorvexus skills
./qorvexus queue
```

Local developer workflows:

```bash
make build
make test
make race
make docker-build
make ci
```

## Social Plugins

Social channels are plugin-based, not core-runtime special cases.

| Plugin | Status | Notes |
| --- | --- | --- |
| Telegram | Available | Polling-first plugin, webhook still supported as an advanced mode |
| Discord | Available | Outbound bot messaging baseline through the Discord REST API |
| Slack | Available | Outbound bot messaging baseline through `chat.postMessage` |

### Telegram

Telegram uses polling by default.

```yaml
social:
  allowed_channels:
    - telegram
  telegram:
    bot_token: "your-telegram-bot-token"
    mode: polling
```

Qorvexus will call Telegram `getUpdates`, ingest messages through the social layer, and send replies back through the Telegram Bot API.

### Discord

Discord currently ships as a clean outbound plugin baseline.

```yaml
social:
  allowed_channels:
    - discord
  discord:
    bot_token: "your-discord-bot-token"
    default_channel_id: "your-channel-id"
```

If a task sends a Discord message without an explicit channel target, Qorvexus will fall back to `social.discord.default_channel_id`.

### Slack

Slack currently ships as a clean outbound plugin baseline.

```yaml
social:
  allowed_channels:
    - slack
  slack:
    bot_token: "your-slack-bot-token"
    default_channel_id: "your-channel-id"
```

If a task sends a Slack message without an explicit target, Qorvexus will fall back to `social.slack.default_channel_id`.

### Manual Social Inbound Test

```bash
curl -X POST http://127.0.0.1:7788/api/social/inbound \
  -H 'Content-Type: application/json' \
  -d '{"channel":"telegram","thread_id":"chat-1","sender_id":"owner","sender_name":"owner","text":"Summarize today and draft replies I should send."}'
```

## Docker

Build the image:

```bash
docker build -t qorvexus:local .
```

Run it with a persistent config and data volume:

```bash
docker run --rm -p 7788:7788 -v "$(pwd)/docker-data:/data" qorvexus:local
```

On first boot the container will generate `/data/qorvexus.yaml`. Edit that file on the host, add your credentials, then restart the container.

## CI And Release

The repository includes:

- `CI` workflow for tests, race tests, formatting checks, builds, and Docker image builds
- `Release` workflow for multi-platform binaries on tags or manual dispatch
- a multi-stage `Dockerfile`
- a small `Makefile` for common local workflows

## Project Layout

```text
cmd/qorvexus           CLI entrypoint
internal/agent         agent loop and tool orchestration
internal/audit         high-impact action logging
internal/cli           bootstrap, commands, runtime wiring
internal/config        config schema and defaults
internal/contextx      context compression
internal/memory        durable note storage
internal/model         provider abstraction and adapters
internal/orchestrator  multi-model discussion
internal/policy        command execution safety rules
internal/scheduler     cron-backed recurring tasks
internal/self          self-improvement backlog and skill management
internal/session       persistent session state
internal/skill         skill discovery and prompt injection
internal/social        envelopes and trust routing
internal/socialplugin  optional social-channel plugins
internal/taskqueue     async work queue and worker
internal/tool          built-in model-visible tools
internal/types         shared protocol types
internal/webui         control panel and HTTP APIs
skills/                workspace skills
docker/                container entrypoint assets
```

## Current Direction

Qorvexus is already usable, but it is still moving toward a stronger product shape:

- richer social plugins
- more model providers
- better long-term memory retrieval
- stronger operational observability
- safer self-improvement workflows

If you want to extend it, the intended path is to add adapters, tools, skills, or plugins without collapsing everything back into one giant runtime file.
