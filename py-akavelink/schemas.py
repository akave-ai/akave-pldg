from datetime import datetime
from enum import Enum
from typing import Optional, List

from pydantic import BaseModel, Field


class JobStatus(str, Enum):
    queued = "queued"
    processing = "processing"
    completed = "completed"
    failed = "failed"


class JobStatusResponse(BaseModel):
    job_id: str
    bucket_name: str
    status: JobStatus
    tx_hash: Optional[str] = None
    error: Optional[str] = None
    created_at: datetime
    updated_at: datetime


class BucketCreateRequest(BaseModel):
    bucket_name: str = Field(
        ...,
        min_length=3,
        max_length=255,
        regex=r"^[a-zA-Z0-9-]+$",
    )


class BucketCreateResponse(BaseModel):
    job_id: str
    bucket_name: str
    status: JobStatus


class BucketDeleteRequest(BaseModel):
    bucket_name: str = Field(
        ...,
        min_length=3,
        max_length=255,
        regex=r"^[a-zA-Z0-9-]+$",
    )


class BucketDeleteResponse(BaseModel):
    job_id: str
    bucket_name: str
    status: JobStatus


class FileUploadRequest(BaseModel):
    bucket_name: str
    file_name: str
    file_path: str


class FileUploadResponse(BaseModel):
    job_id: str
    bucket_name: str
    file_name: str
    status: JobStatus

class FileJobStatusResponse(BaseModel):
    job_id: str
    bucket_name: str
    file_name: str
    status: JobStatus
    root_cid: Optional[str] = None
    encoded_size: Optional[int] = None
    actual_size: Optional[int] = None
    error: Optional[str] = None
    created_at: datetime
    updated_at: datetime

class FileDeleteRequest(BaseModel):
    bucket_name: str
    file_name: str


class FileDeleteResponse(BaseModel):
    job_id: str
    bucket_name: str
    file_name: str
    status: JobStatus


class IPCBucketCreateResult(BaseModel):
    id: str
    name: str
    created_at: int


class IPCBucket(BaseModel):
    id: str
    name: str
    created_at: int


class IPCBucketListItem(BaseModel):
    id: str
    name: str
    created_at: int


class IPCFileMetaV2(BaseModel):
    root_cid: str
    bucket_name: str
    name: str
    encoded_size: int
    size: int
    created_at: float
    committed_at: float


class IPCFileListItem(BaseModel):
    root_cid: str
    name: str
    encoded_size: int
    actual_size: int
    created_at: int


class IPCFileMeta(BaseModel):
    root_cid: str
    name: str
    bucket_name: str
    encoded_size: int
    actual_size: int
    is_public: bool
    created_at: int


class IPCFileChunk(BaseModel):
    cid: str
    size: int
    encoded_size: int
    index: int


class IPCFileDownload(BaseModel):
    bucket_name: str
    name: str
    chunks: List[IPCFileChunk]

    