# Qorvexus

Qorvexus is a from-scratch Go agent runtime designed to stay maintainable while growing into a strong general-purpose autonomous assistant.

Current baseline includes:

- Clear layered architecture: `config`, `model`, `agent`, `tool`, `skill`, `session`, `scheduler`, `contextx`
- OpenAI-compatible model adapter with tool-calling support
- Multimodal message parts plus automatic vision-model fallback
- OpenClaw-style `SKILL.md` loading with YAML frontmatter and basic gating
- Local command execution, HTTP fetch, Playwright command bridge, sub-agent delegation, multi-model consultation, scheduled tasks
- Built-in web control panel for config editing, runtime inspection, queue/session visibility, and ad-hoc execution
- Command policy engine with dangerous-command blocking
- Durable memory store and persistent async task queue
- Social gateway foundations with owner-aware trust boundaries and inbound webhook handling
- Self-improvement primitives for reading config, writing skills, and maintaining a self-evolution backlog
- Audit logging for high-impact actions such as self-modification, task promotion, scheduling, and social sending
- Session persistence plus automatic context compression
- Cron manager for recurring background runs

## Project Layout

```text
cmd/qorvexus           CLI entrypoint
internal/agent         agent loop and tool orchestration
internal/cli           app bootstrap and commands
internal/config        yaml config model and defaults
internal/contextx      context compression
internal/model         provider abstraction and adapters
internal/memory        durable note storage and retrieval
internal/orchestrator  multi-model discussion
internal/policy        command execution safety rules
internal/scheduler     cron-backed task manager
internal/self          self-improvement backlog and skill management
internal/session       persistent session store
internal/skill         skill discovery and prompt injection
internal/social        social-channel envelopes and owner/trust routing
internal/taskqueue     async work queue and worker
internal/tool          tool registry and built-in tools
internal/types         shared protocol types
internal/webui         built-in control panel and HTTP APIs
skills/                workspace skills
qorvexus.yaml          default local runtime configuration
```

## Why This Shape

The goal is not a single hard-coded assistant, but an agent platform:

- Model adapters are isolated so new protocols and distilled-logging interceptors can be added without touching the core loop.
- Tools are first-class and model-visible, so the model can decide when to act.
- Social integrations follow a connector/tool/skill layering similar in spirit to OpenClaw: adapters/connectors handle channel mechanics, tools expose capability, and skills teach the model how to use them.
- Skills are loadable from disk and remain compatible with the `SKILL.md` pattern used by OpenClaw.
- Sessions, compression, and scheduling are explicit subsystems instead of scattered logic.
- Multi-model discussion and child agents are built as orchestration services, not prompt hacks.
- Conversation context can encode channel, sender identity, and trust boundaries so the agent knows when it is talking to the owner versus external parties.
- High-impact self-modification flows can be exposed as tools, while owner-aware context still gates who may trigger them.

## Quick Start

1. Put your API credentials into [qorvexus.yaml](/root/project/qorvexus/qorvexus.yaml).
You can still use environment variables as a fallback, but the config file is the primary place now.
2. Optionally edit [qorvexus.yaml](/root/project/qorvexus/qorvexus.yaml).
If the file does not exist yet, Qorvexus will auto-create a default one on first start.
3. Build:

```bash
go build ./cmd/qorvexus
```

4. Start everything with one command:

```bash
./qorvexus start
```

This starts the web UI, queue worker, scheduler, social watchdogs, and other enabled runtime services.
It also auto-creates a default config file if needed, including a built-in system prompt and default model wiring.
Context compression uses the active conversation model by default, so you do not need to configure a separate summarizer model unless you explicitly want to override that behavior.

5. Run an ad-hoc prompt:

```bash
./qorvexus run "Plan my day and execute any necessary research"

./qorvexus run --image https://example.com/screen.png "Describe this screen and tell me what to do next"
```

6. List loaded skills:

```bash
./qorvexus skills
```

7. Inspect the queue:

```bash
./qorvexus queue
```

8. Telegram webhook endpoint:

```bash
https://your-public-domain.example/webhooks/telegram
```

To use Telegram:

1. Set `social.telegram_bot_token`, `social.public_base_url`, and `social.webhook_secret` in [qorvexus.yaml](/root/project/qorvexus/qorvexus.yaml).
You may still use `telegram_bot_token_env` if you prefer, but it is optional.
2. Start Qorvexus with `./qorvexus start`.
3. Register the webhook with Telegram:

```bash
curl -X POST "https://api.telegram.org/bot<YOUR_TELEGRAM_BOT_TOKEN>/setWebhook" \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://your-public-domain.example/webhooks/telegram",
    "secret_token": "change-me"
  }'
```

When Telegram sends updates to that webhook, Qorvexus will ingest them through the social adapter layer and automatically send the agent reply back through the Telegram Bot API.

9. Manual social-style inbound test:

```bash
curl -X POST http://127.0.0.1:7788/api/social/inbound \
  -H 'Content-Type: application/json' \
  -d '{"channel":"telegram","thread_id":"chat-1","sender_id":"owner","sender_name":"fwerkor","text":"Summarize today and draft replies I should send."}'
```

## Next Expansion Paths

- More model providers and protocol shims
- Richer memory hierarchy and vector retrieval
- Native multimodal ingestion and vision routing
- Permission policies and execution sandboxes
- Observability, traces, and data capture for fine-tuning or distillation
- Long-running background agent workers and inbox/task queues
