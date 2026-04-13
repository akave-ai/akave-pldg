from fastapi import FastAPI, HTTPException, File, UploadFile, Form
from fastapi.responses import StreamingResponse
from datetime import datetime
import uuid
import asyncpg
import os
import io 
import asyncio
import shutil
import tempfile
from worker import create_bucket_task, delete_bucket_task, get_akave_sdk, upload_file_task, delete_file_task
from schemas import BucketCreateRequest, BucketCreateResponse, JobStatus, JobStatusResponse, BucketDeleteRequest, BucketDeleteResponse,  FileUploadResponse, FileDeleteResponse, FileJobStatusResponse, FileDeleteRequest

app = FastAPI(title="py-akavelink")

db_pool = None

@app.on_event("startup")
async def startup():
    global db_pool
    db_pool = await asyncpg.create_pool(
        host=os.getenv("POSTGRES_HOST", "postgres"),
        database=os.getenv("POSTGRES_DB", "akave_platform"),
        user=os.getenv("POSTGRES_USER", "akave"),
        password=os.getenv("POSTGRES_PASSWORD", "password"),
        min_size=5,
        max_size=20
    )
    print("✅ Database connection pool initialized")


@app.on_event("shutdown")
async def shutdown():
    global db_pool
    if db_pool:
        await db_pool.close()
    print("🔒 Database connection pool closed")


@app.get("/")
async def root():
    return {
        "service": "Akave Platform MVP",
        "status": "running",
        "version": "0.1.0"
    }


@app.get("/health")
async def health():
    try:
        async with db_pool.acquire() as conn:
            await conn.fetchval("SELECT 1")
        return {"status": "healthy", "database": "connected"}
    except Exception as e:
        raise HTTPException(status_code=503, detail=f"Database error: {str(e)}")


@app.post("/buckets", response_model=BucketCreateResponse)
async def create_bucket(request: BucketCreateRequest): 
    job_id = str(uuid.uuid4())
    created_at = datetime.now()    
    try:
        async with db_pool.acquire() as conn:
            await conn.execute("""
                INSERT INTO bucket_jobs (id, bucket_name, status, created_at, updated_at)
                VALUES ($1, $2, $3, $4, $5)
            """, job_id, request.bucket_name, "queued", created_at, created_at)
        
        create_bucket_task.delay(job_id, request.bucket_name)
        
        return BucketCreateResponse(
            job_id=job_id,
            bucket_name=request.bucket_name,
            status=JobStatus.queued,
        )
    
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to queue bucket creation: {str(e)}")

@app.post("/buckets/delete", response_model=BucketDeleteResponse)
async def delete_bucket(request: BucketDeleteRequest):
    job_id = str(uuid.uuid4())
    created_at = datetime.now()
    try:
        async with db_pool.acquire() as conn:
            await conn.execute(
                """
                INSERT INTO bucket_jobs (id, bucket_name, status, created_at, updated_at)
                VALUES ($1, $2, $3, $4, $5)
                """,
                job_id,
                request.bucket_name,
                "queued",
                created_at,
                created_at,
            )

        delete_bucket_task.delay(job_id, request.bucket_name)

        return BucketDeleteResponse(
            job_id=job_id,
            bucket_name=request.bucket_name,
            status=JobStatus.queued,
        )

    except Exception as e:
        raise HTTPException(
            status_code=500,
            detail=f"Failed to queue bucket deletion: {str(e)}",
        )

@app.get("/buckets/jobs/{job_id}", response_model=JobStatusResponse)
async def get_job_status(job_id: str):
    
    try:
        async with db_pool.acquire() as conn:
            row = await conn.fetchrow("""
                SELECT id, bucket_name, status, tx_hash, error, created_at, updated_at
                FROM bucket_jobs
                WHERE id = $1
            """, job_id)
        
        if not row:
            raise HTTPException(status_code=404, detail="Job not found")
        
        return JobStatusResponse(
            job_id=str(row['id']),
            bucket_name=row['bucket_name'],
            status=JobStatus(row['status']),
            tx_hash=row['tx_hash'],
            error=row['error'],
            created_at=row['created_at'],
            updated_at=row['updated_at']
        )
    
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Database error: {str(e)}")


@app.get("/buckets")
async def list_buckets():
    try:
        async with db_pool.acquire() as conn:
            rows = await conn.fetch("""
                SELECT bucket_name, tx_hash, created_at
                FROM bucket_jobs
                WHERE status = 'completed'
                ORDER BY created_at DESC
            """)
        
        return {
            "buckets": [
                {
                    "name": row['bucket_name'],
                    "tx_hash": row['tx_hash'],
                    "created_at": row['created_at'].isoformat()
                }
                for row in rows
            ],
            "count": len(rows)
        }
    
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Database error: {str(e)}")


@app.get("/jobs/summary")
async def jobs_summary():
    try:
        async with db_pool.acquire() as conn:
            bucket_rows = await conn.fetch("""
                SELECT status, COUNT(*) as count
                FROM bucket_jobs
                GROUP BY status
            """)
            file_rows = await conn.fetch("""
                SELECT status, COUNT(*) as count
                FROM file_jobs
                GROUP BY status
            """)

        bucket = {row['status']: row['count'] for row in bucket_rows}
        files = {row['status']: row['count'] for row in file_rows}

        return {
            "bucket_jobs": {
                "queued":     bucket.get("queued", 0),
                "processing": bucket.get("processing", 0),
                "completed":  bucket.get("completed", 0),
                "failed":     bucket.get("failed", 0),
            },
            "file_jobs": {
                "queued":     files.get("queued", 0),
                "processing": files.get("processing", 0),
                "completed":  files.get("completed", 0),
                "failed":     files.get("failed", 0),
            },
            "active_jobs": (
                bucket.get("queued", 0) + bucket.get("processing", 0) +
                files.get("queued", 0) + files.get("processing", 0)
            ),
            "total_completed": bucket.get("completed", 0) + files.get("completed", 0),
            "total_failed":    bucket.get("failed", 0) + files.get("failed", 0),
        }

    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Database error: {str(e)}")


@app.get("/jobs/active")
async def active_jobs():
    try:
        async with db_pool.acquire() as conn:
            bucket_rows = await conn.fetch("""
                SELECT id, job_type, bucket_name, status, created_at
                FROM bucket_jobs
                WHERE status IN ('queued', 'processing')
                ORDER BY created_at ASC
            """)
            file_rows = await conn.fetch("""
                SELECT id, job_type, bucket_name, file_name, status, created_at
                FROM file_jobs
                WHERE status IN ('queued', 'processing')
                ORDER BY created_at ASC
            """)

        return {
            "active_count": len(bucket_rows) + len(file_rows),
            "bucket_jobs": [
                {
                    "job_id": str(row['id']),
                    "operation": row['job_type'],
                    "bucket_name": row['bucket_name'],
                    "status": row['status'],
                    "queued_at": row['created_at'].isoformat(),
                }
                for row in bucket_rows
            ],
            "file_jobs": [
                {
                    "job_id": str(row['id']),
                    "operation": row['job_type'],
                    "bucket_name": row['bucket_name'],
                    "file_name": row['file_name'],
                    "status": row['status'],
                    "queued_at": row['created_at'].isoformat(),
                }
                for row in file_rows
            ],
        }

    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Database error: {str(e)}")


@app.post("/files/upload", response_model=FileUploadResponse)
async def upload_file(bucket_name: str = Form(...), file: UploadFile = File(...)):
    job_id = str(uuid.uuid4())
    created_at = datetime.now()
    file_name = file.filename
    
    upload_dir = f"/tmp/akave_uploads/{job_id}"
    os.makedirs(upload_dir, exist_ok=True)
    temp_path = f"{upload_dir}/{file_name}"
    
    try:
        with open(temp_path, "wb") as f:
            shutil.copyfileobj(file.file, f)
            
        async with db_pool.acquire() as conn:
            await conn.execute(
                """
                INSERT INTO file_jobs (id, job_type, bucket_name, file_name, status, created_at, updated_at)
                VALUES ($1, $2, $3, $4, $5, $6, $7)
                """,
                job_id,
                "upload",
                bucket_name,
                file_name,
                "queued",
                created_at,
                created_at,
            )
            
        upload_file_task.delay(job_id, bucket_name, file_name, temp_path) 

        return FileUploadResponse(
            job_id=job_id,
            bucket_name=bucket_name,
            file_name=file_name,
            status=JobStatus.queued,
        )
    
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to upload file: {str(e)}")
    
@app.post("/files/delete", response_model=FileDeleteResponse)
async def delete_file(request: FileDeleteRequest):
    job_id = str(uuid.uuid4())
    created_at = datetime.now()
    
    try: 
        async with db_pool.acquire() as conn:
            await conn.execute(
                """
                INSERT INTO file_jobs (id, job_type, bucket_name, file_name, status, created_at, updated_at)
                VALUES ($1, $2, $3, $4, $5, $6, $7)
                """,
                job_id,
                "delete",
                request.bucket_name,
                file_name,
                "queued",
                created_at,
                created_at,
            )
            
        delete_file_task.delay(job_id, request.bucket_name, request.file_name) 
        return FileDeleteResponse(
                job_id=job_id,
                bucket_name=request.bucket_name,
                file_name=file_name,
                status=JobStatus.queued,
            )
            
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to queue file deletion: {str(e)}")

@app.get("/files/{bucket_name}/{file_name}/download")
async def download_file(bucket_name: str, file_name: str):
    
    def _do_download():
        sdk = get_akave_sdk()
        ipc = sdk.ipc()
        download_manifest = ipc.create_file_download(None, bucket_name, file_name)
        buffer = io.BytesIO()
        ipc.download(None, download_manifest, buffer)
        sdk.close()
        buffer.seek(0)
        return buffer
    
    try:
        buffer = await asyncio.to_thread(_do_download)
        return StreamingResponse(buffer, media_type="application/octet-stream",
                                 headers={
            "Content-Disposition": f"attachment; filename={file_name}"
        })
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to download file: {str(e)}")
    
@app.get("/files/jobs/{job_id}", response_model=FileJobStatusResponse)
async def get_file_job_status(job_id: str):
    try:
        async with db_pool.acquire() as conn:
            row = await conn.fetchrow("""
                SELECT id, bucket_name, file_name, status, root_cid,
                       encoded_size, actual_size, error, created_at, updated_at
                FROM file_jobs
                WHERE id = $1
            """, job_id)

        if not row:
            raise HTTPException(status_code=404, detail="File job not found")

        return FileJobStatusResponse(
            job_id=str(row['id']),
            bucket_name=row['bucket_name'],
            file_name=row['file_name'],
            status=JobStatus(row['status']),
            root_cid=row['root_cid'],
            encoded_size=row['encoded_size'],
            actual_size=row['actual_size'],
            error=row['error'],
            created_at=row['created_at'],
            updated_at=row['updated_at'],
        )

    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Database error: {str(e)}")