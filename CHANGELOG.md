# Changelog

All notable changes to blockrun-llm-go will be documented in this file.

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
