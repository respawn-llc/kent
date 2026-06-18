<p align="center">
  <img src="./docs/public/kent-social-preview.webp" alt="Kent" width="900">
</p>

<p align="center">
  <strong>Kent is a high-performance coding agent for professional Agentic Engineers focusing on output quality and long-running tasks.</strong>
</p>

<p align="center">
  <a href="https://kent.sh/quickstart/">Quickstart</a>
  ·
  <a href="https://kent.sh/docs/">Docs</a>
  ·
  <a href="https://github.com/respawn-llc/kent/releases">Releases</a>
</p>

Kent is a coding agent for professional engineers. It gives frontier coding models the features that empower them to produce their best output: from contextual reminders and harness awareness to token-optimized searches and async execution loops, then wraps that in a UI built for engineers who want to ship real products and work across multiple large codebases.

Codex and Claude Code are good defaults for quick demos and vibe-coding. Kent is for the moment you want the model to work freely but safely, for hours, on large codebases, as your pair programmer & collaborator.

Try it if you have ever lost work quality after compaction, watched an agent hide the command that mattered, babysat a long refactor with "continue" or ralph loops or fixed broken code after the agent ignored repository rules.

<p align="center">
  <img src="./docs/public/readme/kent-demo-hero.webp" alt="Kent running a coding task in the terminal" width="900">
</p>

## Why Kent

### Keeps going when the context gets hard

Compared to other harnesses, Kent has significantly higher compaction quality with its carryover prompts and a multi-step algorithm that **lets the model decide** when and what to compact.
The expensive failure is when model half-remembers a decision, forgets an in-flight edit, or restarts a plan after a bad summary. Kent is designed to make 35+ compactions survivable.

<p align="center">
  <img src="./docs/public/readme/kent-compaction.webp" alt="Kent showing a model-requested compaction handoff in the terminal" width="900">
</p>

### Quality is first-class

- Kent teaches the model to **ask you questions** instead of bulldozing through changes to produce slop. Expect to make important product decisions, learn about caveats, perform refactoring, and ship high-quality code with Kent.
- Kent runs a **customizable supervisor agent** in parallel with the main agent. The supervisor reviews the agent's changes and steers it to follow instructions and do its best work.
- Subagents are real Kent runs. The model delegates to **customizable agent roles, runs everything in async shells**, sleeps and wakes up in a natural, **0-token ralph loop** until the task is done, no matter the scope.

### Token-efficient & cheap

- Smart tool processing. Use **built-in shell optimizers** natively or **connect tools like `rtk`**, and unlike other popular coding agents, the model controls how the output is optimized.
- Unlike harnesses which overload models, Kent ships just **three tools** that enable the model to do everything: `patch`, `shell`, and `ask`. Everything else is smart, contextual, composable, non-blocking.
- Efficient shells. **Tools run async** with the main model: no timeouts, no retries, and compact file-based inspection of shell outputs lead to **1.6-2x token savings**.
- Cache invalidation tracking. Unlike some harnesses that drain your limits in minutes due to a caching bug, with Kent **you know about every unwanted cache miss**. Not that they will happen, with Kent's **snapshot-based cache preservation** mechanisms.
- **Shell-native, scriptable search/read stack** with optimized `rg` config enables **40% more efficient searches** instead of clunky Search, Glob, Grep, Read, Scroll chains.

<p align="center">
  <img src="./docs/public/readme/kent-shells.webp" alt="Kent showing async background shell processes in the terminal" width="900">
</p>

### Everything is customizable & transparent

- Unlike popular harnesses, Kent supports customizing **subagent roles**, **compaction algorithms**, web search, supervisor and main model **system prompts**, skills, tools, reminders, caching, and more.
- With local overrides of everything, create **per-project system prompts**, skill bundles, subagent roles and share the setup with your team via a single `.toml` file.
- The default UI is **fast, non-flickering, native transcript**. Unlike some providers, Kent's detailed mode lets you **inspect every input and output** so there's no surprises, ever.

### True Sandboxing & Parallelization

- Kent runs a single 50mb **server process that orchestrates all your agents** & shells. Unlike other harnesses which embed an unreliable custom sandbox, you can run Kent **completely isolated** (e.g. via Docker) and connect to it from your favorite terminal - no SSH, no tmux.
- Git worktrees are first-class. Run 10+ agents in parallel via **auto-managed worktrees with customizable setup logic**. Unlike other harnesses, the agent knows how to handle the worktree and won't break your repo.

<p align="center">
  <img src="./docs/public/readme/kent-worktrees.webp" alt="Kent switching Git worktrees from the terminal" width="900">
</p>

## Philosophy

Kent is intentionally narrow. It optimizes for engineers who want collaborative workflows, token efficiency, and quality outputs. As such, there will not be:

- MCP support; MCP is suboptimal for shell-enabled agents, use [mcporter](https://github.com/openclaw/mcporter) or native CLIs.
- Plan mode; just prompt the model to plan with you. Kent is built for collaborative work and can plan without any explicit nudges or restrictions.
- WebFetch tool; teach the agent to use [r.jina.ai](jina.ai/reader), browser control CLI, or curl.

Ready to try? Head over to the [quickstart](https://kent.sh/quickstart/) guide.

## Why no Anthropic/Gemini model support?

- Anthropic and Gemini disallow use of third-party harnesses with subscriptions. Using them can get you banned, asking for their support will get people behind Kent sued. Please do not ask for support here.
- Using models with API keys can be supported, but the priority is low due to high costs of testing and significant effort to optimize the harness for different models. Please create or upvote an issue if you want to use Kent with a new provider or model. Kent already supports OpenAI Responses-compatible APIs, local models, and OpenAI API keys.

## License

Kent is licensed under `AGPL-3.0-only`. See [LICENSE](./LICENSE).
