# Technical Implementation Plan: Akave Async Gateway

## System Architecture

The platform uses a **job queue pattern** with three layers working together:

**API Layer (FastAPI)**: Receives HTTP requests, creates job records in PostgreSQL, queues tasks to Redis, and returns immediately with a job_id. Response time stays under 200ms regardless of blockchain latency.

**Queue Layer (Redis)**: Acts as message broker between API and workers. Stores pending jobs and routes them to available workers using Celery's proven distribution mechanism.

**Worker Layer (Celery + Python SDK)**: Background processes that fetch jobs from Redis, execute Akave SDK operations on blockchain, handle retries on failure, and update job status in PostgreSQL with transaction results.

## Database Design

The platform uses a single **bucket_jobs** table to track all operations. Each job has a unique UUID, bucket name, status (queued/processing/completed/failed), optional transaction hash on success, and optional error message on failure.

**Status Flow**: Jobs move through a simple state machine: `queued → processing → completed` or `queued → processing → failed` (after 3 retry attempts).

**Indexes** are placed on status, creation time, and bucket name for fast queries. A unique index on bucket_name (where status = completed) prevents duplicate bucket creation.

```sql
CREATE TABLE bucket_jobs (
    id UUID PRIMARY KEY,
    bucket_name VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    tx_hash VARCHAR(255),
    error TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
```

## API Design

The API exposes four simple REST endpoints:

**POST /buckets**: Creates a bucket job. Generates a UUID, inserts a record with status "queued", sends the task to Celery, and returns the job_id immediately. This is the core async pattern—user gets instant feedback while work happens in background.

**GET /buckets/jobs/{job_id}**: Polls job status. Returns current state (queued/processing/completed/failed), transaction hash if completed, or error message if failed. Users call this repeatedly to track progress.

**GET /buckets**: Lists all successfully created buckets with their transaction hashes and creation timestamps.

**GET /health**: Verifies API and database connectivity for monitoring.

All endpoints use Pydantic models for request/response validation and asyncpg for non-blocking database access, ensuring the API remains fast even under load.

## Worker Implementation

The Celery worker is configured with Redis as both broker and result backend, using JSON serialization for portability. Tasks have a 10-minute timeout with retry logic enabled.

**Task Flow**: When a worker picks up a job, it first updates the database status to "processing", then initializes the Akave Python SDK with configuration from environment variables. It checks if the bucket already exists (to handle idempotency), creates it if needed via `ipc.create_bucket()`, and updates the database with either the blockchain transaction hash or an error message.

**Retry Strategy**: If any exception occurs, the task automatically retries up to 3 times with 10-second delays. This handles transient network issues and blockchain congestion. After exhausting retries, the job is marked as failed with the error details.

**SDK Integration Example**:
```python
@celery_app.task(bind=True, max_retries=3, default_retry_delay=10)
def create_bucket_task(self, job_id: str, bucket_name: str):
    sdk = SDK(SDKConfig(
        address="connect.akave.ai:5500",
        private_key=os.getenv("AKAVE_PRIVATE_KEY"),
        use_connection_pool=True
    ))
    
    ipc = sdk.ipc()
    result = ipc.create_bucket(None, bucket_name)
    
    update_job_status(job_id, "completed", tx_hash=result.id)
    sdk.close()
```

The worker demonstrates **proper SDK usage**: connection pooling for efficiency, explicit close() calls for resource cleanup, and error handling that respects blockchain semantics.

## Docker Deployment

The entire stack runs in Docker using **docker-compose** with four services:

**postgres**: PostgreSQL 15 Alpine image with health checks and volume persistence. The schema.sql file auto-initializes the database on first startup.

**redis**: Redis 7 Alpine as the message broker. Lightweight and fast, perfect for task queuing.

**api**: FastAPI service built from Dockerfile, exposed on port 8000. Waits for healthy postgres and redis before starting.

**worker**: Same Docker image as API but runs `celery worker` command instead. Configured with 4 concurrent workers and depends on healthy database/redis.

The **Dockerfile** uses Python 3.11-slim, installs system dependencies (gcc, git, postgresql-client), installs Python packages from requirements.txt (including Akave SDK from GitHub), and copies the application code.

**Key Dependencies**: FastAPI for async web, Uvicorn for ASGI server, Celery with Redis support, asyncpg for database, and Akave Python SDK from GitHub repository.

## Error Handling & Reliability

The platform handles errors gracefully with automatic retries. Workers attempt each job up to 3 times with 10-second delays between attempts. This covers transient issues like network blips and blockchain congestion.

**Error Categories** include blockchain timeouts, insufficient funds, network failures, and application logic errors. Each failed job stores the complete error message in the database for debugging.

When retries are exhausted, the job is marked as "failed" and the user can see the error via the status endpoint. This transparency helps developers understand what went wrong and how to fix it.

## Performance & Scaling

**Database Connection Pooling**: The API uses asyncpg's connection pool (5-20 connections) to handle concurrent requests efficiently without overwhelming PostgreSQL.

**Worker Scaling**: Each worker handles 4 concurrent jobs. Scale horizontally by running `docker-compose up --scale worker=3` for 12 total concurrent operations.

**Redis Efficiency**: Redis runs with AOF persistence and LRU eviction policy, keeping memory usage under control while ensuring task durability.

## Monitoring & Testing

**Health Monitoring**: Use the `/health` endpoint to verify API and database connectivity. Check worker logs with `docker-compose logs -f worker` to see task processing in real-time.

**Key Metrics** to track: jobs per minute, average processing time, success/failure rates, and active worker count. Query the database to get job statistics: `SELECT status, COUNT(*) FROM bucket_jobs GROUP BY status`.

**Testing Strategy**: Unit tests verify individual endpoints return correct responses. Integration tests create buckets end-to-end and verify completion. Load testing with Apache Bench validates the platform handles high concurrency.

## Security

**Never commit sensitive data**: The `.env` file contains `AKAVE_PRIVATE_KEY` and database credentials. Add it to `.gitignore` and use `chmod 600 .env` for permissions.

**Private Key Storage**: For production, store keys in a separate secure file outside the repository and load via environment variables.

**Network Isolation**: All services run on localhost by default. In production, use proper firewall rules and VPC isolation.

## Deployment Guide

**Local Development**: Copy `.env.example` to `.env`, add your Akave private key, run `docker-compose up -d`. The entire stack starts in under a minute.

**Production Considerations**: Use managed services (RDS for PostgreSQL, ElastiCache for Redis), deploy workers separately for horizontal scaling, add load balancer for API, enable SSL/TLS, and implement proper secrets management for the private key.

**Future: Kubernetes**: The platform is containerized and ready for Kubernetes deployment with separate Deployments for API and workers, ConfigMaps for configuration, and Secrets for sensitive data.

## Future Enhancements

**File Operations**: Extend the pattern to file upload/download with the same async job queue approach. Each file operation becomes a tracked job.

**Apache Iceberg**: Add table management endpoints, leverage Python SDK's data handling capabilities for analytics workloads.

**Webhooks**: Notify external systems when jobs complete, eliminating the need for polling.

**Advanced Monitoring**: Add Prometheus metrics, Grafana dashboards, and OpenTelemetry tracing for production observability.

## Maintenance & Troubleshooting

**Database Migrations**: Use Alembic for schema changes as the platform evolves.

**Backups**: PostgreSQL gets daily backups with point-in-time recovery. Redis uses AOF persistence but can rebuild from PostgreSQL if needed.

**Common Issues**: Worker not processing? Check logs and restart. Database connection errors? Verify connection string. Redis issues? Ping it to verify connectivity.

## Summary

This plan delivers a production-ready async platform in ~4 weeks. The architecture is proven (FastAPI + Celery is industry standard), the Python SDK integration is thorough, and the deployment is simple (one Docker command). Most importantly, it provides a **reference implementation** that shows developers how to build scalable applications with Akave's Python SDK.
