---
title: Integrations
description: Import work from external issue trackers.
---

## GitHub issues

Import an issue as a task:

```sh
kent task import https://github.com/owner/repo/issues/123
```

For issue numbers or inclusive ranges, use `kent task import gh 123,125-127`. Specify `--repo owner/repo` if your workspace has multiple GitHub repositories. Use `--project <project>` to choose a destination other than the project attached to your current directory.

For private issues or more than GitHub's anonymous limit of 60 requests per hour, supply `GH_TOKEN` or `GITHUB_TOKEN`, or sign in with `gh auth login`. Credential priority is:

1. Your CLI environment.
2. The server environment, including its `.env` file.
3. Your CLI machine's GitHub CLI login.
4. The server machine's GitHub CLI login.

Within either environment, `GH_TOKEN` takes precedence over `GITHUB_TOKEN`. Check the account printed by the import command before using the imported tasks. An invalid selected credential fails the import.

Repeated imports skip tasks with the same GitHub source URL in the destination project. Use `--force` to create another task. By default, import assigns matching existing project labels. Use `--labels` to create missing project labels.

A failed batch can create tasks. Inspect the reported task IDs for partial imports before trying again. Run `kent task import --help` for all options.
