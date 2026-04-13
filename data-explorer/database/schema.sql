
CREATE TABLE blocks (
    num           bigint PRIMARY KEY,
    hash          bytea NOT NULL,
    parent_hash   bytea NOT NULL,
    timestamp     timestamptz NOT NULL,
    inserted_at   timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX blocks_hash_idx ON blocks(hash);

CREATE TABLE indexing_state (
    chain_id        text PRIMARY KEY,
    last_indexed    bigint NOT NULL,
    head_seen       bigint NOT NULL,
    reorg_depth     integer NOT NULL DEFAULT 12,
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE contracts (
    address         bytea PRIMARY KEY,
    name            text,
    abi             jsonb,
    inserted_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE actions (
    id              bigint GENERATED ALWAYS AS IDENTITY,

    -- Block/Transaction context
    block_num       bigint NOT NULL,
    block_hash      bytea NOT NULL,
    tx_hash         bytea NOT NULL,
    tx_index        integer NOT NULL,

    -- Transaction sender/receiver
    from_addr       bytea NOT NULL,
    to_addr         bytea,                  -- NULL for contract creation
    
    -- Primary contract (usually tx.to, or created contract)
    contract        bytea NOT NULL,

    -- Decoded transaction input
    method          text,                   -- decoded method name (NULL if can't decode)
    tx_params       jsonb,                  -- decoded transaction parameters
    value           numeric,                -- transaction value in wei

    -- ALL decoded events from this transaction (array of event objects)
    -- Each event object: {event_name, contract, log_index, topics, data}
    events          jsonb,                  -- array of decoded events

    inserted_at     timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (block_num, id)
) PARTITION BY RANGE (block_num);

-- Create initial partitions
CREATE TABLE actions_0_100k
PARTITION OF actions
FOR VALUES FROM (0) TO (100000);

CREATE TABLE actions_100k_200k
PARTITION OF actions
FOR VALUES FROM (100000) TO (200000);


-- BRIN indexes for efficient block range scans (must be on partitions, not parent)
CREATE INDEX actions_0_100k_block_brin ON actions_0_100k USING BRIN (block_num);
CREATE INDEX actions_100k_200k_block_brin ON actions_100k_200k USING BRIN (block_num);

-- B-tree indexes for point lookups (on parent table, inherited by partitions)
CREATE INDEX actions_tx_hash_idx ON actions (tx_hash);
CREATE INDEX actions_contract_idx ON actions (contract);
CREATE INDEX actions_method_idx ON actions (method) WHERE method IS NOT NULL;
CREATE INDEX actions_from_idx ON actions (from_addr);
CREATE INDEX actions_to_idx ON actions (to_addr) WHERE to_addr IS NOT NULL;

-- GIN indexes for JSONB queries
CREATE INDEX actions_tx_params_gin ON actions USING GIN (tx_params jsonb_path_ops) WHERE tx_params IS NOT NULL;
CREATE INDEX actions_events_gin ON actions USING GIN (events jsonb_path_ops) WHERE events IS NOT NULL;
CREATE INDEX actions_events_gin_contain   ON actions USING GIN (events) WHERE events IS NOT NULL;

-- Transactions that could not be decoded during block processing.
-- Kept separate so the main actions table is never polluted with partial rows.
-- status lifecycle: pending → resolved (decoded on retry) | abandoned (max retries exceeded)
CREATE TABLE failed_txs (
    tx_hash         bytea PRIMARY KEY,
    block_num       bigint NOT NULL,
    block_hash      bytea NOT NULL,
    error_reason    text NOT NULL,
    retry_count     integer NOT NULL DEFAULT 0,
    status          text NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'resolved', 'abandoned')),
    inserted_at     timestamptz NOT NULL DEFAULT now(),
    last_attempted  timestamptz
);

CREATE INDEX failed_txs_status_idx    ON failed_txs (status);
CREATE INDEX failed_txs_block_num_idx ON failed_txs (block_num);

-- Composite indexes for common query patterns
CREATE INDEX actions_contract_block_idx ON actions (contract, block_num DESC);
CREATE INDEX actions_from_block_idx ON actions (from_addr, block_num DESC);

CREATE UNIQUE INDEX actions_tx_unique ON actions (block_num, tx_hash);
