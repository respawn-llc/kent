# Tech Debt Research

This folder is the tracked home for Builder tech-debt research.

- `techdebt_criteria.md` is the audit checklist: what to look for during research sweeps.
- `techdebt.md` is the findings catalog: verified debt entries with concrete paths, symbols, implementation impact, and implementation-grade remediation tasks.
- `research_prd.md` is the execution plan for continuing exhaustive research in slices.
- `research_progress.md` tracks slice completion, scratch reconciliation, decisions pass, and final verification.
- `open_questions.md` collects user-facing Q&A decisions that block remediation choices.
- `techdebt_research_proof.md` maps every checked criterion to concrete repository evidence and catalog outcome.
- `research_inventories.md` keeps durable command-output excerpts used by proof paragraphs and reviewer-objection replay.
- `verify_research.py` is the consolidated reproducible verification gate for this folder, including proof order, duplicate-proof, Method/Evidence/Outcome, negative-claim support, and live inventory drift checks for high-risk evidence. It compares `INV-LARGE-FILES`, `INV-PACKAGE-FANOUT`, `INV-SERVER-CLIENTUI-IMPORTS`, and `INV-JSON-TAGS` against live scans, validates `INV-TRANSPORT-FAKES` file/line anchors, checks the live large-file count and `C309` classification coverage, and guards the generated-code/dependency-compatibility mappings in `TD-037`/`TD-035`.

Each catalog entry should include:

- A checklist title line with a `TD-NNN` id and severity marker such as `[P1]`.
- A summary evidence paragraph with exact repo-relative paths, symbols, counts, or reproduction notes.
- An impact paragraph explaining why users, operators, or engineers pay for the debt.
- A remediation task paragraph that fully addresses the problem, including migration/docs/test implications when relevant.
- A regression-prevention paragraph that says what must fail if the debt reappears.

Do not use pseudo-YAML fields such as `Severity:` or `Status:` inside entries.

Prefer one family-wide entry over many duplicate symptom entries. Split entries only when ownership, implementation impact, or remediation task differs.

Run `python3 docs/dev/techdebt/verify_research.py` after any audit update.
