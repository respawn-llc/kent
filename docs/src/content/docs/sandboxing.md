---
title: Sandboxing and security
description: Kent's default trust model, outside-workspace edit prompts, and remote/container server setup.
---

:::warning
Kent tools run with the permissions of the server process. They can access its files, secrets, and network connections.
:::

Run Kent in a container or VM to limit the files, secrets, and networks available to its tools.

## Outside-workspace edits

Kent allows manual edits inside attached workspaces and operating-system temporary directories. Kent requires approval for other paths unless `allow_non_cwd_edits = true`. Edits in other Kent-managed worktrees are blocked.

## Container image shape

A Kent sandbox image should contain:

- A `kent` binary compatible with the client version you use.
- Mandatory server dependencies: shell, `rg` and `git`, for normal operation of the server.
- A `config.toml` file with your setup.
- Optional tools: language toolchains, package managers, `fd`, `jq`, `patch`, `curl`, `gh`, `wget`, `python` and project-specific CLIs.
- An (ideally persistent) workspace directory such as `/workspace`.
- A writable Kent persistence root, usually under the sandbox user's home.
- Network access limited to what the task requires.

Mount only the workspace, caches, and credentials required for the task.

## Example Dockerfile

Add the language runtimes and project tools your workflows require.

```dockerfile
FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive
ENV HOME=/home/kent
ENV SHELL=/bin/bash
ARG KENT_VERSION=

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    bash \
    ca-certificates \
    curl \
    fd-find \
    file \
    git \
    jq \
    less \
    netcat-openbsd \
    openssh-client \
    patch \
    procps \
    python3 \
    python3-pip \
    python3-venv \
    ripgrep \
    tar \
    tini \
    unzip \
    xz-utils \
    zip \
  && ln -sf /usr/bin/fdfind /usr/local/bin/fd \
  && useradd --create-home --shell /bin/bash kent \
  && mkdir -p /workspace /home/kent/.kent \
  && chown -R kent:kent /workspace /home/kent

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN curl -fsSL https://kent.sh/install.sh \
  | KENT_PREFIX=/usr/local KENT_VERSION="${KENT_VERSION}" sh

USER kent
WORKDIR /workspace
EXPOSE 53082

ENTRYPOINT ["tini", "--"]
CMD ["kent", "serve"]
```

The image installs the latest release by default. Pin a release with `docker build --build-arg KENT_VERSION=vX.Y.Z -t kent-sandbox .`.

Run the server so it listens inside the container and is reachable from the host:

```bash
docker run --name kent-sandbox --rm -it \
  -p 127.0.0.1:53082:53082 \
  -e KENT_SERVER_HOST=0.0.0.0 \
  -e KENT_SERVER_PORT=53082 \
  -v "$PWD:/workspace" \
  kent-sandbox
```

In another terminal, point the local client at that server:

```bash
export KENT_SERVER_HOST=127.0.0.1
export KENT_SERVER_PORT=53082
kent project create --path /workspace --name sandbox
kent
```

The project path is `/workspace` because that is the path visible to the server.
