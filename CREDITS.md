# Credits and Acknowledgements

## The Foundation: NOFX

EvolvX is built directly on top of [NOFX](https://github.com/NoFxAiOS/nofx) by the [NoFxAiOS team](https://github.com/NoFxAiOS).

**Without NOFX, EvolvX does not exist.**

Every line of AI trading machinery in this repository — the multi-model AI client architecture, the exchange adapters for Binance/Bybit/OKX/Hyperliquid/Aster DEX/Lighter, the strategy prompt builder with chain-of-thought parsing, the indicator pipeline (EMA/MACD/RSI/ATR with TA-Lib), the AI Debate Arena, the AI500 coin scoring integration, the entire React dashboard, the authentication system, and the multi-trader management layer — is the original work of the NOFX authors and contributors.

```
Project:    NOFX — Open-source AI Trading OS
Repository: https://github.com/NoFxAiOS/nofx
Stars:      10,000+
Forks:      2,700+
License:    GNU Affero General Public License v3.0 (AGPL-3.0)
```

### Please star the original

If EvolvX has been useful to you, the most meaningful thing you can do is **star the original NOFX repository**:

👉 [**github.com/NoFxAiOS/nofx**](https://github.com/NoFxAiOS/nofx)

The NOFX team built something genuinely innovative. They deserve the recognition.

---

## EvolvX Contributions

The architectural layers added by EvolvX:

| Layer | Files | Description |
|---|---|---|
| Unified Pipeline | `engine/core/`, `engine/adapters/`, `engine/pipeline/`, `engine/feeds/` | Single execution path for all three modes |
| Strategy Registry | `registry/` | Immutable, versioned, semver strategy artifacts |
| Decision Journal | `journal/` | Durable per-decision memory with outcome tracking |
| Optimizer | `optimizer/` | Walk-forward candidate generation and scoring |
| Integration | `trader/auto_trader_pipeline.go`, `api/registry_handlers.go`, `api/services.go` | Wiring the new layers into the existing app |
| Migration | `cmd/migrate/` | One-time import from existing NOFX databases |

### Author

```
Joerg Peetz
Senior Technical Support Engineer, Trend AI (formerly Trend Micro)
Ireland

Email:     j.peetz69@gmail.com
GitHub:    https://github.com/JPeetz
X:         https://x.com/joerg_peetz
LinkedIn:  https://linkedin.com/in/joerg-peetz
Medium:    https://medium.com/@jpeetz
```

Also the author of:
- **NeuralLog** — AI-powered log analysis with MCP integration for technical support operations
- **AutoNovelClaw** — autonomous novel-writing pipeline (55-file open-source Python project)
- **crypt0safety** — crypto security alert platform on X

---

## Dependencies

EvolvX inherits all of NOFX's dependencies and adds:

| Package | Purpose | License |
|---|---|---|
| `Masterminds/semver/v3` | Semantic version parsing and comparison | MIT |
| `google/uuid` | UUID generation for all entity IDs | BSD-3-Clause |
| `mattn/go-sqlite3` | SQLite driver (already used by NOFX) | MIT |
| `stretchr/testify` | Test assertions | MIT |

Full dependency list: see [go.mod](go.mod).

---

## License Compliance

EvolvX is licensed under **AGPL-3.0**, the same license as NOFX. This is required by the AGPL license terms — modifications to AGPL-licensed software must also be released under AGPL.

The practical meaning: if you run EvolvX as a service that others access over a network, you must make the full source code (including your modifications) available to those users. The license text is in [LICENSE](LICENSE).

If you're running EvolvX privately for your own trading, the AGPL does not require you to publish your modifications.
