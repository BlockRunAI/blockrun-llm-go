# Changelog

All notable changes to blockrun-llm-go will be documented in this file.

## 0.3.0

- **New image model: `openai/gpt-image-2`** (ChatGPT Images 2.0). Reasoning-driven generation with multilingual text rendering + character consistency. Pricing: $0.06 (1024²), $0.12 (1536×1024 / 1024×1536). Also supported by `client.Edit()` alongside `gpt-image-1`.
- **New video models: 3 ByteDance Seedance variants** on `VideoClient`:
  - `bytedance/seedance-1.5-pro` — $0.03/sec, 720p, 5s default (up to 10s).
  - `bytedance/seedance-2.0-fast` — $0.15/sec, ~60-80s generation, sweet-spot price/quality.
  - `bytedance/seedance-2.0` — $0.30/sec, 720p Pro quality.
  All support text-to-video and image-to-video. No SDK surface change — pass the model ID via `VideoGenerateOptions.Model`.
- `video.go`: `VideoClient` doc comment no longer claims xAI-only.
- `image.go` + `video.go`: header comments enumerate available models + pricing for discoverability.
- README: Image line mentions gpt-image-2; Video section now a proper table listing all 4 models; new Video feature row in the capability matrix.

## 0.2.1

- **NVIDIA free-tier refresh (backend 2026-04-21).** Retired `nvidia/nemotron-*`, `nvidia/mistral-large-3-675b`, `nvidia/devstral-2-123b`, `nvidia/qwen3.5-397b-a17b`, and paid `nvidia/kimi-k2.5`. The routing table in `router.go` now references the survivors + the two new models:
  - `RoutingFree`: Simple → `nvidia/gpt-oss-120b`, Medium → `nvidia/deepseek-v3.2`, Complex / Reasoning → `nvidia/qwen3-next-80b-a3b-thinking`.
  - `RoutingEco` / `RoutingAuto` Simple: `nvidia/kimi-k2.5` → `moonshot/kimi-k2.5` (canonical target of the backend redirect).
- Backend still redirects the old IDs, so upgrading is non-breaking; `SmartChat` just now names the right model in its output.
- README Smart Routing table refreshed.

## 0.2.0

- **New market-data client** (`market.go`) — `LLMClient.Price`, `.History`, and `.ListSymbols` over the Pyth-backed endpoints. Crypto, FX and commodity are fully free (price + history + list); stocks across 12 markets (us/hk/jp/kr/gb/de/fr/nl/ie/lu/cn/ca) and the `usstock` legacy alias charge for price + history, list stays free. The client handles x402 transparently on either path.
- New types: `PricePoint`, `PriceBar`, `PriceHistoryResponse`, `SymbolListResponse`, `PriceCategory`, `PriceOptions`, `HistoryOptions`, `ListOptions`.
- `ChatMessage` gains optional `ReasoningContent` and `Thinking` fields for reasoning-capable upstreams (DeepSeek Reasoner, Grok 4 / 4.20 reasoning).
- `Usage` gains optional `CacheReadInputTokens` / `CacheCreationInputTokens` for Anthropic prompt-caching telemetry.
- `Model` gains optional `BillingMode` (`paid`/`flat`/`free`), `FlatPrice`, `Categories`, `Hidden` so the `/v1/models` payload can round-trip.
- Internal: `baseClient.doGetWithPayment` + `handleGetPaymentAndRetry` support x402 on GET endpoints (previously only POST was handled).
- First tracked `VERSION` / `CHANGELOG.md`.
