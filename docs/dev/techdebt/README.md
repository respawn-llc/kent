# Tech Debt Backlog

This folder is the tracked home for Builder tech-debt remediation backlog.

- `techdebt.md` is the findings catalog: verified debt entries with concrete paths, symbols, implementation impact, and implementation-grade remediation tasks.
- Product decisions that constrain remediation live under `docs/dev/specs/`, not in this folder.

Each catalog entry should include:

- A checklist title line with a `TD-NNN` id and severity marker such as `[P1]`.
- A summary evidence paragraph with exact repo-relative paths, symbols, counts, or reproduction notes.
- An impact paragraph explaining why users, operators, or engineers pay for the debt.
- A remediation task paragraph that fully addresses the problem, including migration/docs/test implications when relevant.
- A regression-prevention paragraph that says what must fail if the debt reappears.

Do not use pseudo-YAML fields such as `Severity:` or `Status:` inside entries.

Prefer one family-wide entry over many duplicate symptom entries. Split entries only when ownership, implementation impact, or remediation task differs.

After editing the catalog, verify concrete paths and symbols with repository searches before handing off.
