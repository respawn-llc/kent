# WebP alpha regression fixtures

These are the byte-exact artifacts from Kent issue #308 and deepteams/webp issue #4, retained in gist `Nek-12/5a1154432ca59cea7e3af09b7fbdf6e4`.

- `issue-308.png`: valid transparent source PNG. SHA-256: `311ee393dcb6bcc2b331a4f201ab5195129c3dae5eb62a3d8021fee4f9334f0b`.
- `issue-308-invalid.webp`: invalid compressed-alpha output produced by deepteams/webp v1.2.2. SHA-256: `6ac31a3892989b226201d9338782ad02f66432043b11c06696e7de38e7df013e`.

The source image checks successful optimization and alpha preservation through an independent decoder. The invalid WebP checks rejection even when optimization is disabled.
