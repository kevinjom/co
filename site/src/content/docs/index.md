---
title: Overview
description: co is a coding assistant for the directory you start it in.
---

:::caution
**Status:** actively under development and not stable. Interfaces, behavior, and the on-disk config may change without notice.
:::

`co` is a coding assistant for the directory you start it in. It talks to OpenAI-compatible chat endpoints, and the model may list, read, search, create, and edit files inside that directory. Those file tools cannot leave the directory, including through symlinks. The model may also run a shell command. That command can read the rest of the disk and use the network. It can write anywhere the user can.

A terminal on both stdin and stdout gets a Bubble Tea TUI, which can hold more than one session. A pipe or a redirect gets a line reader with one session.

## Start here

- [Install](install/) `co`, or run it locally with Ollama
- [Providers](setup/providers/), [API keys](setup/api-keys/), and [settings](setup/settings/)
- [Sessions and commands](use/sessions/)
- [Tools](use/tools/), including [bash](tools/bash/) and the [language server](tools/lsp/)
