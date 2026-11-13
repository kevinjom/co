---
title: API keys
description: Where co reads an API key, and how to save one with /connect.
---

A key is never stored in the YAML. `co` checks `api_key_env`, then `~/.co/credentials.json`. The credentials file is mode `0600` and its directory is mode `0700`.

In the session, `/connect` lists providers that accept a key. `/connect openrouter` prompts for the key and stores it. The prompt hides what you type. Outside a terminal, set the environment variable instead:

```sh
OPENROUTER_API_KEY=... co --model openrouter/qwen/qwen3-coder
```

`api_key_env` is set on the provider. See [Providers](../providers/).
