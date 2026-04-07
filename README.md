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
- Session persistence plus automatic context compression through a summarizer model
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
internal/session       persistent session store
internal/skill         skill discovery and prompt injection
internal/taskqueue     async work queue and worker
internal/tool          tool registry and built-in tools
internal/types         shared protocol types
internal/webui         built-in control panel and HTTP APIs
skills/                workspace skills
examples/              sample configuration
```

## Why This Shape

The goal is not a single hard-coded assistant, but an agent platform:

- Model adapters are isolated so new protocols and distilled-logging interceptors can be added without touching the core loop.
- Tools are first-class and model-visible, so the model can decide when to act.
- Skills are loadable from disk and remain compatible with the `SKILL.md` pattern used by OpenClaw.
- Sessions, compression, and scheduling are explicit subsystems instead of scattered logic.
- Multi-model discussion and child agents are built as orchestration services, not prompt hacks.

## Quick Start

1. Set `OPENAI_API_KEY`.
2. Edit [examples/qorvexus.yaml](/root/project/qorvexus/examples/qorvexus.yaml).
3. Build:

```bash
go build ./cmd/qorvexus
```

4. Run:

```bash
./qorvexus run --config examples/qorvexus.yaml "Plan my day and execute any necessary research"

./qorvexus run --config examples/qorvexus.yaml --image https://example.com/screen.png "Describe this screen and tell me what to do next"
```

5. List loaded skills:

```bash
./qorvexus skills --config examples/qorvexus.yaml
```

6. Start the scheduler daemon:

```bash
./qorvexus daemon --config examples/qorvexus.yaml
```

7. Start the web panel:

```bash
./qorvexus web --config examples/qorvexus.yaml
```

8. Inspect the queue:

```bash
./qorvexus queue --config examples/qorvexus.yaml
```

## Next Expansion Paths

- More model providers and protocol shims
- Richer memory hierarchy and vector retrieval
- Native multimodal ingestion and vision routing
- Permission policies and execution sandboxes
- Observability, traces, and data capture for fine-tuning or distillation
- Long-running background agent workers and inbox/task queues
