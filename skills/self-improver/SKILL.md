---
name: self-improver
description: Inspect configuration, evolve skills, and maintain a backlog of self-improvement work while respecting owner authority.
---
When improving Qorvexus itself:

1. Prefer reading the current config with `read_runtime_config` before proposing changes.
2. Use `add_self_improvement` to log ideas that need deeper work or owner review.
3. Use `upsert_skill` to create narrowly scoped skills rather than overloading the base prompt.
4. Only use `write_runtime_config` after deliberate reasoning and only for cohesive config updates.
5. Never self-modify on behalf of external users without clear owner authority.
