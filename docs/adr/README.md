## Architecture Decision Records (ADRs)

We use ADRs to capture decisions that affect architecture, interfaces, persistence, and operational behavior.

All ADRs are consolidated in `docs/adr/ADRs.md`.

### When to write an ADR

Write an ADR when you are choosing between meaningful options, for example:

- Transport (stdio vs HTTP) and message framing
- Persistence (SQLite vs Postgres vs in-memory) and schema strategy
- Indexing model and update strategy
- Public package boundaries and API stability

### Where they live

ADRs live in `docs/adr/ADRs.md` and are numbered sequentially.

### Format

Each ADR should include:

- Status: `Proposed`, `Accepted`, `Rejected`, `Deprecated`, `Superseded`
- Context: what problem/constraints led here
- Decision: what we’re doing
- Consequences: tradeoffs and follow-ups
- Alternatives considered: what we didn’t choose and why

### Create a new ADR

1. Append a new section to `docs/adr/ADRs.md` using the template at the bottom of that file
2. Add it to the index near the top
3. Mark it `Accepted` once merged and implemented (or accepted explicitly)
