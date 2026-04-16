# Migrating from NOFX to EvolvX

## Overview

Migration is **additive and non-destructive**. Your existing `nofx.db` is never modified. All existing features continue to work identically. The migration reads your existing data and writes it into new database files.

---

## Pre-migration checklist

- [ ] NOFX is running and healthy
- [ ] You have a backup of `nofx.db` (always a good idea before any database operation)
- [ ] Go 1.21+ is installed
- [ ] You've cloned EvolvX

---

## Step 1: Clone and build EvolvX

```bash
git clone https://github.com/JPeetz/EvolvX.git
cd EvolvX
go mod download
go build -o evolvx ./...
```

---

## Step 2: Run the migration tool

```bash
go run ./cmd/migrate/main.go \
  --legacy-db    /path/to/nofx.db   \
  --registry-db  ./registry.db       \
  --journal-db   ./journal.db        \
  --import-decisions

# Expected output:
# migrate: imported 4 strategies, skipped 0
# migrate: imported 1,247 legacy decision records
# migration complete.
```

The tool probes for either `auto_traders` or `strategies` as the source table and either `decision_logs` or `decision_records` as the decision table, so it works with multiple NOFX versions.

---

## Step 3: Verify the import

```bash
# Check strategies were imported
sqlite3 registry.db "SELECT id, name, version, status FROM strategies;"

# Check decisions were imported  
sqlite3 journal.db "SELECT COUNT(*) FROM decisions;"
```

---

## Step 4: Enable the unified pipeline

In your `.env` or config file, add:

```env
USE_UNIFIED_PIPELINE=true
REGISTRY_DB_PATH=./registry.db
JOURNAL_DB_PATH=./journal.db
OPTIMIZER_DB_PATH=./optimizer.db
```

---

## Step 5: Restart and verify

```bash
./evolvx
# or
docker compose up -d
```

Visit `http://localhost:3000` — the existing UI is unchanged. Verify trading continues normally.

Check the new endpoints are live:

```bash
curl http://localhost:3000/api/v1/registry/strategies \
  -H "Authorization: Bearer YOUR_TOKEN"
# Should return your migrated strategies
```

---

## Rollback

If anything looks wrong, set `USE_UNIFIED_PIPELINE=false` and restart. The original NOFX code path resumes immediately. The new databases are not used. Your `nofx.db` was never touched.

---

## What migrates automatically

| Data | Migrated? | Notes |
|---|---|---|
| Strategy configs | ✅ | Each auto_trader becomes a `registry.StrategyRecord` at `v1.0.0` |
| Decision logs | ✅ | Each row becomes a `journal.DecisionEntry` |
| Outcomes | ⚠️ | Imported as `outcome_class="pending"` — historical outcomes aren't reliably reconstructable from raw logs |
| Klines / market data | ✅ | Already in nofx.db, used directly by the historical feed |
| User accounts | ✅ | Already in nofx.db, unchanged |
| Exchange API keys | ✅ | Already in nofx.db, unchanged |

---

## Post-migration: improving the imported data

After migration, your imported strategies will be at `v1.0.0 / StatusDraft`. To improve the data:

```bash
# Review imported strategies
curl http://localhost:3000/api/v1/registry/strategies/{id}/versions

# If your strategy was already running live, promote it to approved
curl -X PUT http://localhost:3000/api/v1/registry/strategies/{id}/versions/1.0.0/status \
  -d '{ "status": "approved", "changed_by": "j.peetz69@gmail.com" }'

# Record outcomes for past decisions (optional, improves AI memory quality)
# This can be done in batch from your trade history
curl -X POST http://localhost:3000/api/v1/journal/decisions/{id}/outcome \
  -d '{ "class": "win", "realized_pnl": 48.50, ... }'
```
