# CrossChain Archive: Bridge Transaction & Message Indexer on Akave O3

**Status**: Ready to submit the plan
**Contributor**: Patrick-Ehimen
**Date**: February 3, 2026
**Note**: Open for potential collaborators

## Problem Statement

Cross-chain bridges and messaging protocols (Wormhole, LayerZero, Axelar, Chainlink CCIP) move billions of dollars and millions of messages across blockchains. However, tracking this activity is fragmented and challenging:

- **No unified view**: Each protocol has its own explorer, requiring users to check multiple sources
- **Hard to trace end-to-end**: Correlating a source transaction to its destination execution requires manual effort
- **Data disappears**: RPC nodes prune old data, making historical analysis difficult
- **Forensics gaps**: When bridges are exploited, investigators lack comprehensive historical archives
- **No immutable record**: Existing indexers run on centralized infrastructure with no data integrity guarantees

There is a clear need for a **unified, immutable, and queryable archive** of cross-chain activity that spans multiple protocols and chains.

## Related Projects & References

- **Wormhole** - Cross-chain messaging protocol with Guardian network
  https://wormhole.com/
- **LayerZero** - Omnichain interoperability protocol (V1 & V2)
  https://layerzero.network/
- **Axelar** - Cross-chain communication network with GMP
  https://axelar.network/
- **Chainlink CCIP** - Cross-Chain Interoperability Protocol
  https://chain.link/cross-chain
- **Existing Explorers**:
  - Wormholescan: https://wormholescan.io/
  - LayerZero Scan: https://layerzeroscan.com/
  - Axelarscan: https://axelarscan.io/

## Objective

Build a **cross-chain indexer and archive** that:

1. Indexes bridge transactions and cross-chain messages from **multiple protocols**
2. Normalizes data into a **unified schema** for cross-protocol queries
3. Provides **end-to-end message tracing** (source TX → destination TX)
4. Archives all data immutably on **Akave O3**
5. Exposes a **query API** for developers, researchers, and analytics platforms

This project validates **Akave O3** as an immutable archive layer for high-volume, multi-chain blockchain data.

## Core Idea

Users should be able to query cross-chain activity as:

- **Messages** (protocol-agnostic cross-chain communications)
- **Transfers** (token movements across chains)
- **Traces** (full end-to-end transaction lifecycle)

Instead of:
> "Check Wormholescan for VAA status, then check destination chain explorer for execution"

They should see:
> "Message 0xabc: LayerZero packet from Ethereum → Arbitrum, status: Executed, source TX: 0x123, dest TX: 0x456, latency: 45s"

## Scope

### In Scope

- **Multi-protocol indexer** supporting:
  - Wormhole (VAAs, token transfers)
  - LayerZero V2 (packets, OFT transfers)
  - Axelar (GMP calls, token transfers)
  - Chainlink CCIP (messages)
- **Multi-chain support**:
  - Ethereum, Arbitrum, Optimism, Base, Polygon, Avalanche, BSC
- **Data normalization** into unified schema
- **End-to-end correlation** linking source and destination transactions
- **Status tracking** (pending, confirmed, executed, failed)
- **Akave O3 archival** for immutable storage
- **Query API** (REST/GraphQL) for:
  - Message lookup by ID/hash
  - Filtering by chain, protocol, status, time range
  - Address history (all cross-chain activity for an address)
  - End-to-end trace retrieval
- **Documentation** and reproducible setup

### Out of Scope

- Real-time websocket subscriptions (future enhancement)
- Full historical backfill from genesis (start from recent blocks)
- Custom UI/frontend (API-first approach, UI can be Phase 2)
- Non-EVM chains (Solana, Cosmos can be future scope)
- Bridge security monitoring/alerting

## Intended Users / ICP

- **Bridge users** tracking stuck or pending transactions
- **Security researchers** analyzing bridge exploits and patterns
- **Protocol teams** monitoring cross-chain message health
- **Analytics platforms** building cross-chain dashboards
- **Wallets and explorers** showing users their cross-chain history
- **Compliance teams** tracing fund flows across chains

## High-Level Architecture

### Ingestion Layer (Go)

- Connects to multiple chain RPCs (Ethereum, Arbitrum, Base, etc.)
- Listeners for each protocol:
  - **Wormhole**: `LogMessagePublished`, `TransferRedeemed` events
  - **LayerZero**: `PacketSent`, `PacketReceived`, `OFTSent` events
  - **Axelar**: `ContractCall`, `ContractCallApproved`, `Executed` events
  - **CCIP**: `CCIPSendRequested`, `ExecutionStateChanged` events
- Handles reorgs and confirmation depths
- Batches events for processing

### Normalization Layer

Unified message schema:
```
{
  message_id: string,
  protocol: "wormhole" | "layerzero_v2" | "axelar" | "ccip",
  type: "token_transfer" | "message" | "gmp_call",
  source: {
    chain_id: number,
    tx_hash: string,
    block_number: number,
    timestamp: number,
    sender: string
  },
  destination: {
    chain_id: number,
    tx_hash: string | null,
    block_number: number | null,
    timestamp: number | null,
    receiver: string
  },
  status: "pending" | "confirmed" | "executed" | "failed",
  payload: {
    token?: string,
    amount?: string,
    data?: string,
    nonce?: number
  },
  metadata: {
    fee: string,
    relayer?: string,
    gas_used?: number,
    latency_seconds?: number
  }
}
```

### Storage Layer

**PostgreSQL** (hot data / queries):
- Fast lookups by message ID, tx hash, address
- Status tracking and updates
- Aggregations and analytics queries

**Akave O3** (cold data / archive):
- Raw event logs in Parquet format
- Daily/hourly snapshots
- Full message payloads
- Immutable historical record

Storage schema on O3:
```
crosschain-archive/
├── protocols/
│   ├── wormhole/
│   │   └── {chain}/{year}-{month}.parquet
│   ├── layerzero/
│   ├── axelar/
│   └── ccip/
├── unified/
│   └── messages/{year}-{month}.parquet
├── aggregated/
│   ├── daily_volume.parquet
│   └── route_stats.parquet
└── manifests/
    └── index.json
```

### Query API

REST/GraphQL endpoints:
- `GET /messages/{message_id}` - Get message by ID
- `GET /messages?src_chain=1&dst_chain=42161&protocol=layerzero&status=pending`
- `GET /transactions/{tx_hash}/messages` - All messages in a transaction
- `GET /address/{addr}/history` - Cross-chain history for address
- `GET /trace/{message_id}` - Full end-to-end trace with all events
- `GET /protocols/{protocol}/stats` - Volume, latency, success rate
- `GET /routes/stats` - Popular routes, volume by chain pair

## Expected Deliverables

- **crosschain-indexer** (Go service):
  - Multi-chain RPC connections
  - Protocol-specific event decoders
  - PostgreSQL indexing
  - Akave O3 archival pipeline
- **crosschain-api** (Go service):
  - REST API with OpenAPI spec
  - GraphQL endpoint (optional)
  - Query optimization for common patterns
- **Database schema** + migrations
- **Documentation**:
  - Setup and deployment guide
  - API reference
  - Adding new protocols guide
  - Architecture overview
- **Testing**:
  - Unit tests for decoders
  - Integration tests with testnet data
  - CI pipeline

## Success Criteria

- Successfully indexes messages from **4 protocols** (Wormhole, LayerZero, Axelar, CCIP)
- Supports **5+ chains** (Ethereum, Arbitrum, Base, Optimism, Polygon)
- **End-to-end tracing** correctly correlates source and destination transactions
- Historical data queryable from **Akave O3** archives
- API response time **< 500ms** for single message lookup
- **Documentation** sufficient for new contributors to add protocols
- Demonstrates clear value for cross-chain data archival use case

## Validation Goals

- Validate **Akave O3** as an archive layer for high-volume blockchain data
- Demonstrate practical multi-chain, multi-protocol indexing with O3 backend
- Showcase a **cross-chain infrastructure** use case aligned with Akave's ICP
- Provide a reusable pattern for blockchain data archival on decentralized storage
- Enable security researchers and analysts with immutable, verifiable data

## Iterative Development Plan

This project is designed for phased delivery:

1. **Phase 1: Core Infrastructure**
   - Multi-chain RPC connector
   - PostgreSQL schema
   - Akave O3 client wrapper
   - Single protocol decoder (LayerZero V2)

2. **Phase 2: Protocol Expansion**
   - Add Wormhole decoder
   - Add Axelar decoder
   - Add CCIP decoder
   - End-to-end correlation logic

3. **Phase 3: API & Query Layer**
   - REST API implementation
   - Message tracing endpoints
   - Address history queries
   - Analytics endpoints

4. **Phase 4: Production Hardening**
   - Reorg handling
   - Backfill tooling
   - Performance optimization
   - Comprehensive documentation

## References

- **Akave O3 Console**: https://console.akave.ai/
- **Wormhole Docs**: https://docs.wormhole.com/
- **LayerZero Docs**: https://docs.layerzero.network/
- **Axelar Docs**: https://docs.axelar.dev/
- **Chainlink CCIP Docs**: https://docs.chain.link/ccip
