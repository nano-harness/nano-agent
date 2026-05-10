# Sub-agent model routing

Sub-agents reuse the lead agent's multi-provider routing configuration from `.nano.yaml`: `providers`, top-level `fallbacks`, and `model_routing`.

## Resolution rules

- No sub-agent `model`: inherit the lead agent primary route and fallback chain.
- Sub-agent `model` only: use that model as the primary route, while inheriting the lead fallback chain.
- Sub-agent `model` plus `fallbacks`: use the sub-agent model and fallback list as the complete route chain.

Sub-agent `model` and `fallbacks` values are provider/model references, such as `deepseek/deepseek-chat` or `openai/gpt-4.1`, resolved through the lead config's `providers` block.

## Circuit breakers

Sub-agents share circuit breaker instances with the lead agent by provider and base URL. If one agent opens the breaker for a provider endpoint, all agents route away from that unhealthy endpoint faster.

## Subprocess mode

Subprocess teammates start through the hidden `nano teammate` command and read `.nano.yaml` from the working directory. Provider inheritance depends on launching the subprocess from the correct project directory. The `--model` flag can still override the primary model; fallback chains are loaded from configuration/profile data rather than passed as command-line flags.

## Migrating old examples

Do not use `model_base_url` or `model_api_key` under sub-agent definitions. Define endpoint and credential data in the top-level `providers` block, then reference models by `provider/model` in `.nano/agents/*.md`.
