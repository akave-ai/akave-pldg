[![data-explorer CI](https://github.com/DarkLord017/akave-pldg/actions/workflows/data-explorer-ci.yml/badge.svg?branch=main)](https://github.com/DarkLord017/akave-pldg/actions/workflows/data-explorer-ci.yml)

# Data Explorer — Storage.sol-Aware Blockchain Indexer for Akave

A domain-specific indexer and API built on top of the Akave public RPC that translates raw `Storage.sol` contract activity into human-readable, queryable storage actions — uploads, updates, deletions, registrations — instead of generic transactions.

> **Part of the [Akave PLDG](https://github.com/DarkLord017/akave-pldg)**

---

## The Problem

Akave's existing explorer ([explorer.akave.ai](https://explorer.akave.ai), powered by Blockscout) is great for generic blockchain exploration, but it exposes storage contract activity only as:

- Raw transactions and method selectors
- Encoded input data
- Uninterpreted log events

---

## The Solution

**Data Explorer** is an indexer + API service that:

1. Connects to the Akave public RPC
2. Pulls blocks and filters transactions to `Storage.sol` contract activity
3. Decodes method calls and emitted events using the contract ABI
4. Stores normalized, domain-meaningful records in a database
5. Exposes a queryable REST API for storage-centric views

Instead of:
> `"Transaction 0xabc… called contract 0xdef…"`

You get:
> `"Storage.uploadFile() by 0x…, CID=…, size=…, bucket=…, status=…"`

---

## Architecture

```
Akave Public RPC
      │
      ▼
┌─────────────┐      ┌──────────────────┐      ┌─────────────┐
│   Indexer   │─────▶│   Database (DB)  │◀─────│  API Layer  │
│   (Go)      │      │ Postgres/SQLite  │      │  REST/JSON  │
└─────────────┘      └──────────────────┘      └─────────────┘
```

## Reorg handling 

<img width="1582" height="720" alt="image" src="https://github.com/user-attachments/assets/e22529a7-a795-482b-a7e6-9cac1a027bb0" />

### Indexer (Go)

- Connects to the Akave public RPC endpoint
- Tracks latest indexed block with reorg-safe windowing
- Filters transactions to `Storage.sol` contract address
- Decodes function selectors → method names (via ABI)
- Decodes input parameters and emitted events into typed structures

### Database

- PostgreSQL (preferred) or SQLite for MVP
- Core tables: `contracts`, `methods`, `events`, `actions`, `indexing_state`

### API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/actions?method=<methodName>` | Transactions by method |
| GET | `/actions?paramKey=<key>&paramValue=<value>` | Transactions by decoded parameter (supports nested params) |
| GET | `/actions?event_name=<eventName>` | Events by event name |
| GET | `/actions?event_data_key=<key>&event_data_val=<value>` | Events by decoded parameter |
| GET | `/actions?from=<startBlock>&to=<endBlock>` | Complete block range |

### Demo Screenshots

- **View complete transaction**  
  <img width="1469" height="878" alt="View complete transaction" src="https://github.com/user-attachments/assets/0f148395-3524-402b-a7b7-27d8766dfdc9" />

- **Filter by transaction parameters**  
  <img width="1469" height="878" alt="Filter by transaction parameters" src="https://github.com/user-attachments/assets/7eed1027-9011-451b-be87-12187e2c5eb1" />

- **Filter by event name**  
  <img width="1469" height="878" alt="Filter by event name" src="https://github.com/user-attachments/assets/07d17476-3df6-4661-95b2-52ece5e4403f" />

- **Filter by event parameters**  
  <img width="1469" height="878" alt="Filter by event parameters" src="https://github.com/user-attachments/assets/b909d395-8d19-4514-b6f7-45358a0d144b" />

- **Filter by block range**  
  <img width="1469" height="878" alt="Filter by block range" src="https://github.com/user-attachments/assets/a49c7312-b865-4fc9-88ba-99cb942763fc" />

---

## Getting Started

### Prerequisites

- Go 1.21+
- PostgreSQL (or SQLite for local dev)
- Access to Akave public RPC

### Network Info

| Resource | Value |
|----------|-------|
| Public RPC | `https://c6-us.akave.ai/ext/bc/56g16Hr1SHQRzdM8JLm3GKYv7APVHY8T2TyeZLvDVzCaTRS7W/rpc` |
| Explorer (Blockscout) | https://explorer.akave.ai |
| Faucet | https://faucet.akave.ai |

### Setup

```bash
# Clone the repo
git clone https://github.com/DarkLord017/akave-pldg.git
cd akave-pldg/data-explorer

# Create a .env file with the required environment variables
cat > .env <<EOF
POSTGRES_USER=indexer
POSTGRES_PASSWORD=indexer_password
POSTGRES_DB=blockchain_explorer
DB_HOST=postgres
DB_USER=indexer
DB_PASSWORD=indexer_password
DB_NAME=blockchain_explorer
RPC_URL=https://c6-us.akave.ai/ext/bc/56g16Hr1SHQRzdM8JLm3GKYv7APVHY8T2TyeZLvDVzCaTRS7W/rpc
BACKFILL_FROM=0
BACKFILL_TO=0
API_ADDR=:8080
EOF

# Build and run with Docker
make start
```

> This RPC URL is exposed to allow open contribution while the project is under active development.
# Things left to do at the end of cohort
- E2E testing
- db optimisations
- frontend & analytics

---

## Resources

- [Storage.sol ABI](https://github.com/akave-ai/akavesdk/blob/main/private/ipc/contracts/storage.go#L75)
- [Akave Docs](https://docs.akave.ai)
- [Decoding non-indexed events (Go)](https://ethereum.stackexchange.com/questions/28637/how-to-decode-log-data-in-go)
- [RLP decoding transactions (go-ethereum)](https://github.com/ethereum/go-ethereum/blob/master/core/types/transaction.go)
- [Tracking Issue #1](https://github.com/DarkLord017/akave-pldg/issues/1)
