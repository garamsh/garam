# PLAN.md — Template

Canonical `PLAN.md` structure. Use this template top-to-bottom when materialising a plan to a file. The "write `N/A`" rule applies to **Medium / Large** plans in `.opencode/PLAN.md`; **Small / Trivial** plans (typically emitted as conversation messages by `code-plan`) may omit non-REQUIRED sections entirely. Structure stays consistent; reviewers know where to look.

## Contents
- Structure (top to bottom, mandatory skeleton)
- Required vs. recommended sections
- Scale to task size
- Anti-patterns in plan content

---

## Structure (top to bottom, mandatory skeleton)

```
# PLAN.md — <Short Title>
> Status · Owner · Created · Last updated · Related · Stack reference
## 1. Goal — one testable sentence
## 2. Context — why now, current state, constraints, dependencies
## 3. Non-Goals — explicit omissions
## 4. Proposed Solution
   4.0 Library & Tooling Choices — slot / chosen / version / rationale
   4.1 Approach
   4.2 Interfaces — concrete signatures + sample payloads
   4.3 Data Model — schema + concrete example
   4.4 Implementation Steps — ≤ 1 hour each, names file + goal trace
   4.5 Edge Cases & Error Handling
## 5. Alternatives Considered — ≥ 1 alternative
## 6. Cross-Cutting Concerns — security, privacy, observability,
    performance, compatibility, reliability (each: answer or N/A)
## 7. Test & Verification Plan
   7.1 Automated Tests — unit / integration / e2e
   7.2 Manual Verification — executable commands + pass criterion
   7.3 Definition of Done — checklist
## 8. Risks & Open Questions — risks with mitigations; open questions
    with stated defaults
## 9. Deviations Log — empty for new plans; populated during run
## Revision Log
```

---

## Required vs. recommended sections

| Section | Class | Why |
|---------|:-----:|-----|
| Metadata header | REQUIRED | Without status/owner, the plan is orphan data. |
| 1. Goal | REQUIRED | Without a single testable intent, the agent optimizes the wrong thing. |
| 2. Context | REQUIRED | Anchors the plan to the existing codebase; prevents "rewrite from scratch" gold-plating. |
| 3. Non-Goals | REQUIRED | Single most effective defense against scope creep. |
| 4. Proposed Solution (incl. Interfaces, Steps) | REQUIRED | Without concrete proposal + interfaces, agents hallucinate types and contracts. |
| 5. Alternatives Considered | RECOMMENDED | Forces articulation of *why this* and not something else. |
| 6. Cross-Cutting Concerns | RECOMMENDED | Forces explicit pass over security / observability / compat. |
| 7. Test & Verification Plan | REQUIRED | "It compiled" is not done. Verification is the only proof of done. |
| 8. Risks & Open Questions | RECOMMENDED | Surfaces unknowns instead of letting agents paper over them. |
| 9. Deviations Log | OPTIONAL | Valuable; can be re-derived from git, but keeping it is faster. |
| Revision Log | OPTIONAL | Tracks plan revisions and the context that drove each. Cheap to maintain. |

If a REQUIRED section is genuinely empty, write `**N/A** — <reason>`
instead of skipping it.

---

## Scale to task size

| Size | Signals | Sections to include |
|------|---------|---------------------|
| **Trivial** | 1-line fix, 1 test tweak, typo | 1 (Goal) + 7 (Test Plan), 2 sentences of context |
| **Small** | Single function or bug, ≤2 files | 1, 2, 4.1, 4.2, 7 |
| **Medium** | Feature, several files, new module | All sections (1–9) |
| **Large** | Architectural change, new service, multi-phase | All sections (1–9) + links to ADRs; split into one PLAN.md per phase |

When in doubt, write a Medium plan. Over-scoping a small task is
cheap; under-scoping a medium task is expensive.

---

## Anti-patterns in plan content

- No goal statement → agent optimizes for the wrong metric.
- No non-goals → agent implements every adjacent plausible feature.
- No interfaces section → agent invents types that drift from the system.
- Vague steps ("add caching") → agent picks an arbitrary strategy.
- Verification without executable commands → agent stops at "it compiled."
- Cross-cutting concerns omitted → agent ships with no logs / auth / rollback.
- Alternatives as an afterthought → signals indecision; weakens the path.
- Speculative future sections → agent treats them as current requirements.