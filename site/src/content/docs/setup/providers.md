---
title: Providers
description: How co names models and stores providers in YAML.
---

Providers live in YAML. A model is `provider/model-id`. The model id may contain further slashes, as in `openrouter/qwen/qwen3-coder`.

`~/.co/models.yaml` is the provider catalog. A generated file starts with Gemini and OpenRouter. [`co ollama`](../../install/#run-locally-with-ollama) adds the `ollama` provider beside them:

```yaml
providers:
  gemini:
    base_url: https://generativelanguage.googleapis.com/v1beta/openai
    api_key_env: GEMINI_API_KEY
    models:
      - gemini-flash-latest
  openrouter:
    base_url: https://openrouter.ai/api/v1
    api_key_env: OPENROUTER_API_KEY
    models:
      - qwen/qwen3-coder
  ollama:
    base_url: http://127.0.0.1:11434
    auth: none
    models:
      - qwen3:8b
    compat:
      reasoning_effort: true
      reasoning_delta: true
```

`base_url` is required. If it already ends in `/v1` or `/openai`, `co` calls `/chat/completions` on it. Otherwise it calls `/v1/chat/completions`, which is what an Ollama host needs.

`auth: none` means the provider takes no API key. Any other provider needs one. `api_key_env` names an environment variable that is checked first. Keys are on [API keys](../api-keys/).

`compat.reasoning_effort` sends `reasoning_effort: low` on each request. `compat.reasoning_delta` reads a `reasoning` field from stream chunks and shows it while the model thinks. `co ollama` turns both on when it creates the provider.

The default model and the other settings live in [Settings](../settings/).
