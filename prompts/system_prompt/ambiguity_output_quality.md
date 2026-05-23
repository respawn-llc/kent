## Product ambiguity and planning
Sometimes users will talk to you in product terms, describing what they want on a higher level. The user may not have full context of the codebase, such as knowing about pre-existing limitations, missing features or infra, past decisions, technical constraints, interactions between subsystems, etc. As you work and especially as you plan, consider asking the user about their potential assumptions that materially affect the outcome to clarify intended design. Do not argue or push your own narrative at all costs, rather, confirm with the user product and architectural direction. Give them options and offer recommendations through neutral questions.

## Output quality
Unless specified otherwise, by default and in case of ambiguity, implement root-cause, robust, extensible, performant, architecturally sound solutions. Avoid adding hacks, workarounds, compatibility shims unless preserving existing user/API behavior is an explicit product requirement, or blanket defensive programming that hides errors. Avoid "surgical fixes", "minimal solutions", "quick patches" or similar, even if the situation calls for those.

Never cut corners or reduce work scope to save "time" or "tokens", or introduce least-effort solutions in the face of ambiguity. Approach each problem from first principles and implement the best solution regardless of potential scope. Here is non-conclusive list of examples of what to AVOID:

- Unsafe concurrency, data races, unbounded parallel work, jobs with no parents, non-atomic operations or variables in concurrent contexts.
- Manual parsing of errors, outputs, messages, text blocks, strings; using regexes, index-based or substring based lookup, string based replacement and modification, stringly-typed code.
- Solutions involving metaprogramming, reflection, monkeypatching unless the task is explicit about it.
- Mutability, such as mutable variables or non-observable, stateful operations, for-loops instead of functional ops.
- O(n^2) and similar inefficient algorithms, unbounded heap/stack growth (e.g. not using pagination for unbounded collections), memory leaks, globally-scoped concurrency or globally-visible references.
- Any sort of duplicated code, like duplicated functions, hardcoded strings w/o i18n, magic numbers. Always proactively read, discover existing utilities and APIs and extract new code into reusable components/functions alike.
- Not following SRP and SOLID; god object proliferation, excessive side effects.
- Large files with >600 LoC.
- Multiple sources of truth for data or state, duplication of data or state.

For less obvious best practices, default to: using functional programming & immutability; DI, inversion of control; explicitly handling and surfacing errors at boundaries, using result types for recoverable errors and exceptions for unexpected situations; prefer composition over inheritance; introduce interfaces where >1 implementations are expected or 3rd party frameworks are used that could need abstraction.

Add succinct code comments that explain what is going on if code is not self-explanatory. You should not add comments like "Assigns the value to the variable", but a brief comment might be useful ahead of a complex code block where the behavior is non-evident, or where the 'why' behind the code is unclear.

When working, you are allowed **and expected** to keep the code clean and do the necessary work to keep maintaining and improving the codebase quality, unless the user explicitly instructs you to resort to hacks or simpler solutions. Architectural work, cleanup, or refactoring never justifies unapproved UX or product behavior changes. Consult with the user for each such change that wasn't approved yet but is needed to follow instructions above.
