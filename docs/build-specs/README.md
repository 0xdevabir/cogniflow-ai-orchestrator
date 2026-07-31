# CogniFlow — Per-Phase Build Specs

> **This is the file you hand to an AI builder.** Each phase below is a complete, self-contained build instruction: what to create, what each function does, the contracts, the tests, and the demo moment.
>
> Pair with [`../phase-roadmap.md`](../phase-roadmap.md) for the high-level summary and [`../architecture.md`](../architecture.md) for the deep design.

## How to use this

When you say *"build phase X"*, paste the matching spec (Phase 1 → `phase-1-providers-chat.md`, etc.) along with:

1. This `README.md` (gives orientation)
2. The current repo state (`apps/`, `packages/`, `docs/` already exist from Phase 0)
3. The relevant phase file

Every spec follows the same structure:

| Section | Purpose |
|---|---|
| **Goal** | One sentence, the demo moment |
| **Prerequisites** | Which phases must be done first |
| **Files to create** | Exact paths, by service |
| **Per-file contracts** | Function signatures, types, behaviors |
| **Tests to write** | Concrete test names + expected outcomes |
| **Wire-up steps** | Code-level glue (env vars, service registration, etc.) |
| **End-to-end verification** | Exact curl / UI flow that proves it works |
| **Done checklist** | Acceptance criteria |

## File map

| Phase | Spec |
|---|---|
| 0 | Already complete — see [`../phase-roadmap.md`](../phase-roadmap.md) |
| 1 | [`phase-1-providers-chat.md`](phase-1-providers-chat.md) |
| 2 | [`phase-2-decomposer.md`](phase-2-decomposer.md) |
| 3 | [`phase-3-router.md`](phase-3-router.md) |
| 4 | [`phase-4-dag-executor.md`](phase-4-dag-executor.md) |
| 5 | [`phase-5-fusion-citations.md`](phase-5-fusion-citations.md) |
| 6 | [`phase-6-rag.md`](phase-6-rag.md) |
| 7 | [`phase-7-eval-budget.md`](phase-7-eval-budget.md) |
| 8 | [`phase-8-polish.md`](phase-8-polish.md) |
