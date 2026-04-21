# Changelog

All notable changes to blockrun-llm-go will be documented in this file.

## 0.2.0

- **New market-data client** (`market.go`) — `LLMClient.Price`, `.History`, and `.ListSymbols` over the Pyth-backed endpoints. Crypto, FX and commodity are fully free (price + history + list); stocks across 12 markets (us/hk/jp/kr/gb/de/fr/nl/ie/lu/cn/ca) and the `usstock` legacy alias charge for price + history, list stays free. The client handles x402 transparently on either path.
- New types: `PricePoint`, `PriceBar`, `PriceHistoryResponse`, `SymbolListResponse`, `PriceCategory`, `PriceOptions`, `HistoryOptions`, `ListOptions`.
- `ChatMessage` gains optional `ReasoningContent` and `Thinking` fields for reasoning-capable upstreams (DeepSeek Reasoner, Grok 4 / 4.20 reasoning).
- `Usage` gains optional `CacheReadInputTokens` / `CacheCreationInputTokens` for Anthropic prompt-caching telemetry.
- `Model` gains optional `BillingMode` (`paid`/`flat`/`free`), `FlatPrice`, `Categories`, `Hidden` so the `/v1/models` payload can round-trip.
- Internal: `baseClient.doGetWithPayment` + `handleGetPaymentAndRetry` support x402 on GET endpoints (previously only POST was handled).
- First tracked `VERSION` / `CHANGELOG.md`.
