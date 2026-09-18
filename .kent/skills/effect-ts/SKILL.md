---
name: effect-ts
description: Build and change desktop screens with Effect, Atom React and TanStack Query. Proactively use when implementing ViewModels, reactive state, async actions, resource lifetimes or native event streams in the desktop app.
---

## Read the installed guidance
Paths below are relative to the repository root unless stated otherwise.

Read `apps/desktop/node_modules/effect/AGENTS.md` completely before writing Effect code. It is the maintainers' usage guide for the installed version; follow its linked examples for the task at hand. Resolve API questions in the installed source instead of guessing from Effect 3 examples.

These topic references are relative to `apps/desktop/node_modules`:

- Effects, typed failures and Promise boundaries: `effect/ai-docs/src/01_effect/01_basics/10_creating-effects.ts` and `effect/ai-docs/src/01_effect/04_errors/`.
- Resource acquisition and finalization: `effect/ai-docs/src/01_effect/05_resources/10_acquire-release.ts`.
- Cold callbacks, buffering and consumption: `effect/ai-docs/src/03_stream/`.
- Atom evaluation, actions and disposal: `effect/src/unstable/reactivity/Atom.ts`; React ownership and bindings: `@effect/atom-react/src/`.
- Effect test ownership: `effect/ai-docs/src/09_testing/10_effect-tests.ts`.

Repository rules in `apps/AGENTS.md` govern adoption boundaries, validation, execution and checks; apply those constraints when upstream examples use broader runtime, service or transport architectures.

## Build a destination
1. Identify the owning destination, its identity and remount boundary, and the approved behavior for reads, drafts, requests and navigation. Read the corresponding product specification before choosing new behavior.
2. Trace the Project Edit reference from `apps/desktop/src/features/project-edit/ProjectEditRoute.tsx` through `ProjectEditViewModel.ts`. Read `apps/desktop/src/app/sidebarStack.ts` when working on destination lifetime or completion navigation.
3. Define the feature's ViewModel factory from its actual dependencies and stable destination identity. Construct descriptions and observers there; acquiring native resources and subscribing belong to mounted Atom evaluation. Keep the model stable for that destination's lifetime, including unrelated navigation callback changes.
4. Expose readonly observations and typed actions. Keep draft values local and derive validation, changed-state and displayed state from their authoritative inputs. Leave fetched results, pending/error state, pagination and mutation completion with Query.
5. Observe real Query observers through `apps/desktop/src/app-facade/queryAtom.ts`. Retain the Query result object and its lifetime semantics. Extend the shared integration only when a real feature requires it.
6. Connect actions through the standard `@effect/atom-react` bindings under the owned provider in `apps/desktop/src/app/AppProviders.tsx`. Follow `useProjectEditActions` for mounting request observations independently of optional visual readers.
7. Keep JSX focused on rendering and user input. Preserve existing React forms, Zod validation and native bridge/API boundaries.

## Implement actions and observations
- Express sequential local work with the installed Effect generator APIs. Convert Promise failures into typed Effect errors at the external boundary; keep diagnostics available to the existing status surface.
- Check action admission against actual Query observer state synchronously. Read the reference's concurrent Atom actions together with `requests` and `requestPending`; the action option alone does not establish request ownership.
- Put mutation feedback, invalidation and completion that must survive navigation in Query callbacks. Pass invocation-specific navigation callbacks with the action input when their identity can change during the destination lifetime.
- Use a cold `Stream.callback` for native registration, with scoped acquisition/release and explicit failure of the supplied queue when registration fails. Consider delayed registration as well as normal unlisten.
- Filter relevant native changes before a bounded buffer. Coalesce only notifications whose contents can be reconstructed from authoritative reads; preserve explicit actions and confirmation choices.
- Handle errors at the owner that can present them. Query read failures remain observable Query results. Distinguish a failed read from a failed stream registration before choosing where an Effect catch belongs.
- Preserve library-owned disposal and mutation completion. Consult `apps/AGENTS.md` before adding execution roots, subscription contracts, retry behavior or lifecycle machinery.

## Verify the feature boundary
Use the approved task testing approach and repository Just commands. Follow `ProjectEditViewModel.test.tsx`, `ProjectEditRoute.test.tsx` and `ProjectDeleteButton.test.tsx` in `apps/desktop/src/features/project-edit` for product-boundary coverage with real Query and Atom ownership.

Select cases affected by the change: duplicate admission, invalid input, draft preservation, explicit retry, bounded pagination, multiple readers, final disposal, delayed registration, and accepted mutation completion after navigation. Use dedicated lint fixtures for policy changes. Report what ran and any unverified native behavior; examples in this skill do not expand the task's testing scope.
