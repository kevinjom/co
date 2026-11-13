---
title: Install
description: Install co with the install script, from a checkout, or with go install.
---

```
curl -fsSL https://raw.githubusercontent.com/kevinjom/co/main/install.sh | sh
curl -fsSL https://raw.githubusercontent.com/kevinjom/co/main/install.sh | sh -s -- 0.1.0
```

To build from a checkout, Go 1.26.4 or newer:

```sh
git clone https://github.com/kevinjom/co.git
cd co
go build -o co ./cmd/co

# or run directly
go run ./cmd/co
```

The same toolchain installs the latest commit:

```sh
go install github.com/kevinjom/co/cmd/co@latest
```

## Run locally with Ollama

With the `ollama` binary installed:

```sh
co ollama
```

This talks to `http://127.0.0.1:11434`, starts a local daemon when it is down, and asks you to pick a model when none is already loaded. It adds an `ollama` provider to `~/.co/models.yaml` and leaves every other provider in that file, writes the chosen model into `~/.co/config.yaml`, and, in a terminal, opens the session on that model.

```sh
co ollama --host http://127.0.0.1:11434
```

`--host` wins. Otherwise `co ollama` uses `OLLAMA_HOST`, then `http://127.0.0.1:11434`. A host without a scheme is treated as `http://`. `co` only starts the daemon for `127.0.0.1`, `localhost`, and `::1`.

The provider catalog is on [Providers](../setup/providers/).
