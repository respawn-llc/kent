# RTK Gradle Post-Processing Inventory

RTK source inspected at `/Users/nek/Developer/Tools/rtk`, commit `55f998d08cd80ece970fe5e61eaae3533512288b`.

This tracked note mirrors key conclusions from ignored scratch audit `docs/tmp/rtk-post-processing-audit.md`, section `Build / Test / Lint / Format Families` -> `5. Gradle / Android family`.

## Layers

- Explicit wrapper: `rtk gradlew ...` in `src/cmds/jvm/gradlew_cmd.rs`.
- Hook/rewrite rule: `./gradlew`, `gradlew.bat`, `gradlew`, and `gradle` route recognized Gradle task invocations toward `rtk gradlew`.
- Generic TOML fallback: `src/filters/gradle.toml` provides weaker display-only stripping for raw fallback commands that match its regex.

## Source Anchors

- Wrapper CLI declaration/routing: `src/main.rs:730-735`, `src/main.rs:2143-2145`.
- Raw fallback/TOML path: `src/main.rs:1131-1255`.
- Rewrite rule: `src/discover/rules.rs:629-635`.
- Wrapper task routing: `src/cmds/jvm/gradlew_cmd.rs:12-18`, `src/cmds/jvm/gradlew_cmd.rs:20-63`, `src/cmds/jvm/gradlew_cmd.rs:65-174`.
- Wrapper output filters: `src/cmds/jvm/gradlew_cmd.rs:176-520`.
- Built-in TOML filter: `src/filters/gradle.toml:1-35`.
- TOML filter engine stages: `src/core/toml_filter.rs:425-533`.

## Wrapper Behavior

- Verbose/debug flags bypass filtering: `--stacktrace`, `--info`, `--debug`, `--full-stacktrace`.
- Task detection uses last non-flag, non-`clean` arg. Mixed tasks use last task.
- Caveat: task detection is not full Gradle CLI parsing. Non-flag option values can be misclassified as tasks when they are separate argv entries. Inline flag values like `-Pflavor=testRelease` are ignored because they start with `-`.
- Build tasks stream line-filtered output, keeping status, actionable task count, errors, warnings, build scan links, and blank separators while stripping task/progress/config/daemon/Try noise.
- Unit tests keep failed test lines, exception/message lines starting `java.` or `kotlin.`, first non-framework user stack frame, test summaries, build status, and actionable task count; passed/skipped tests and framework frames are stripped.
- Connected tests strip instrumentation/device noise, special-case `No connected devices!`, then reuse unit-test filtering.
- Lint keeps Android lint errors/warnings with up to 3 context lines, ktlint/detekt single-line violations, summaries, build status, and actionable task count; report paths are stripped.
- Dependencies output becomes top-level dependency coordinates grouped by configuration; transitive deps and Gradle boilerplate are stripped.

## TOML Fallback Behavior

- Match regex is exactly `^(gradle|gradlew|\./)gradlew?\b`.
- Regex caveat: because `gradlew?` is outside the alternation group, it can overmatch beyond obvious Gradle prefixes, including strings shaped like `gradlegradle`, `gradlegradlew`, `gradlewgradle`, `gradlewgradlew`, `./gradle`, and `./gradlew`.
- Filter strips ANSI, removes common progress/task/cache/daemon lines, truncates remaining lines to 150 chars, caps output at 50 lines, and returns `gradle: ok` if empty.

## Caveats

- Build mode is streaming, so it is per-line keep/drop rather than multi-line structured parsing.
- Unit test filtering keeps only first user-code stack frame after each failed test.
- Lint context retention is Android-lint-specific; ktlint/detekt entries are single-line.
- TOML filter is weaker than wrapper and should be treated as display-only.
