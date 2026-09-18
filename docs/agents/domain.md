# Domain Docs

Engineering skills consume this repo's domain documentation as follows.

## Before exploring, read these

- `CONTEXT.md` at the repo root, if it exists.
- `docs/adr/` entries that touch the area being changed, if they exist.

If these files do not exist, proceed silently. The domain-modeling skill creates them lazily when terms or decisions are resolved.

## File structure

This is a single-context repo:

```
/
├── CONTEXT.md
├── docs/adr/
└── src/
```

## Use the glossary vocabulary

When output names a domain concept, use the term defined in `CONTEXT.md`. If the needed concept is missing, note the gap for domain modeling.

## Flag ADR conflicts

If output contradicts an existing ADR, surface the conflict explicitly instead of silently overriding it.
