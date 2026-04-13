CREATE TABLE IF NOT EXISTS bucket_jobs (
    id UUID PRIMARY KEY,
    bucket_name VARCHAR(255) NOT NULL,
    job_type VARCHAR(50) NOT NULL DEFAULT 'create',  -- create, delete
    status VARCHAR(50) NOT NULL,                      -- queued, processing, completed, failed
    tx_hash VARCHAR(255),                             -- Blockchain transaction hash
    error TEXT,                                       -- Error message if failed
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_bucket_jobs_status ON bucket_jobs(status);
CREATE INDEX IF NOT EXISTS idx_bucket_jobs_created ON bucket_jobs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_bucket_jobs_bucket_name ON bucket_jobs(bucket_name);

CREATE UNIQUE INDEX IF NOT EXISTS idx_bucket_jobs_unique_completed
ON bucket_jobs(bucket_name)
WHERE status = 'completed' AND job_type = 'create';


CREATE TABLE IF NOT EXISTS file_jobs (
    id UUID PRIMARY KEY,
    job_type VARCHAR(50) NOT NULL,      -- upload, delete
    bucket_name VARCHAR(255) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,        -- queued, processing, completed, failed
    root_cid VARCHAR(255),              -- CID returned on successful upload
    encoded_size BIGINT,
    actual_size BIGINT,
    error TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_file_jobs_status ON file_jobs(status);
CREATE INDEX IF NOT EXISTS idx_file_jobs_created ON file_jobs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_file_jobs_bucket ON file_jobs(bucket_name);
CREATE INDEX IF NOT EXISTS idx_file_jobs_bucket_file ON file_jobs(bucket_name, file_name);