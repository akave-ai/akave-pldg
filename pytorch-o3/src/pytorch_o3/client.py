import os
import io
import logging

from akavesdk import SDK, SDKConfig, SDKError
from tenacity import retry, wait_exponential, stop_after_attempt

from .exceptions import O3AuthError

logger = logging.getLogger(__name__)

class O3Client:
    def __init__(self, private_key=None, ipc_address="connect.akave.ai:5500"):
        self.private_key = private_key or os.getenv('AKAVE_PRIVATE_KEY')
        self._ipc_address = ipc_address
        if not self.private_key:
            raise O3AuthError("AKAVE_PRIVATE_KEY is missing.")
            
        try:
            config = SDKConfig(
                address=ipc_address,
                max_concurrency=10,
                block_part_size=1_000_000,
                private_key=self.private_key,
                use_connection_pool=True,
                connection_timeout=30
            )
            self.sdk = SDK(config)
            self.ipc = self.sdk.ipc()
        except SDKError as e:
            raise O3AuthError(f"SDK Error: {e}")
        except Exception as e:
            raise O3AuthError(f"Init Error: {e}")
        
    def create_bucket(self,name):
        return self.ipc.create_bucket(None, name)

    @retry(wait=wait_exponential(multiplier=1, min=2, max=10), stop=stop_after_attempt(5)) 
    def list_buckets(self):
        try:
            return self.ipc.list_buckets(None, offset=0, limit=0)
        except Exception as e:
            raise e
    
    def list_objects(self, bucket_name: str, prefix: str = "", limit: int = 1000):
        if hasattr(self.ipc, 'list_files'):
            files = self.ipc.list_files(None, bucket_name)
            
            if prefix:
                original_count = len([f for f in files if hasattr(f, 'name')])
                files = [f for f in files if hasattr(f, 'name') and f.name.startswith(prefix)]
                if len(files) < original_count:
                    logger.warning("Some objects may have been filtered out due to missing 'name' attribute")
            
            if limit and limit > 0:
                files = files[:limit]
            
            return files
        else:
            raise NotImplementedError("akavesdk IPC interface needs list_files method")
    
    def get_object_info(self, bucket_name: str, key: str):
        if hasattr(self.ipc, 'file_info'):
            info = self.ipc.file_info(None, bucket_name, key)
            if info is None:
                raise RuntimeError(f"File {key} not found in bucket {bucket_name}")
            return info
        else:
            raise NotImplementedError("akavesdk IPC interface needs file_info method")
    
    def _download_full(self, bucket_name: str, key: str) -> bytes:
        """Internal method to download a full object."""
        if not (hasattr(self.ipc, 'create_file_download') and hasattr(self.ipc, 'download')):
            raise NotImplementedError("akavesdk IPC interface needs create_file_download and download methods")
        file_download = self.ipc.create_file_download(None, bucket_name, key)
        buffer = io.BytesIO()
        self.ipc.download(None, file_download, buffer)
        return buffer.getvalue()

    @retry(wait=wait_exponential(multiplier=1, min=2, max=10), stop=stop_after_attempt(3))
    def download_object_range(self, bucket_name: str, key: str, start: int, end: int) -> bytes:
        """Download a byte range from an object.

        Args:
            bucket_name: Name of the bucket
            key: Object key/filename
            start: Start byte offset
            end: End byte offset

        Returns:
            Requested byte range as bytes
        """
        if hasattr(self.ipc, 'create_range_file_download'):
            file_download = self.ipc.create_range_file_download(None, bucket_name, key, start, end)
            buffer = io.BytesIO()
            self.ipc.download(None, file_download, buffer)
            return buffer.getvalue()
        else:
            # Fallback: download full and slice
            full_data = self._download_full(bucket_name, key)
            return full_data[start:end]

    @retry(wait=wait_exponential(multiplier=1, min=2, max=10), stop=stop_after_attempt(3))
    def download_object(self, bucket_name: str, key: str) -> bytes:
        """Download a full object from O3 storage.

        Args:
            bucket_name: Name of the bucket
            key: Object key/filename

        Returns:
            Full object data as bytes
        """
        return self._download_full(bucket_name, key)

    @retry(wait=wait_exponential(multiplier=1, min=2, max=10), stop=stop_after_attempt(3))
    def upload_object(self, bucket_name: str, key: str, data: bytes):
        """Upload an object to O3 storage.

        Args:
            bucket_name: Name of the bucket
            key: Object key/filename
            data: Bytes to upload (minimum 127 bytes)

        Returns:
            FileMeta object containing root_cid and other metadata

        Raises:
            ValueError: If data is less than 127 bytes (Akave minimum)
            NotImplementedError: If SDK doesn't support upload
        """
        if len(data) < 127:
            raise ValueError(f"Data must be at least 127 bytes (got {len(data)}). Akave O3 minimum file size requirement.")

        if hasattr(self.ipc, 'upload'):
            file_obj = io.BytesIO(data)
            file_meta = self.ipc.upload(None, bucket_name, key, file_obj)
            return file_meta
        else:
            raise NotImplementedError("akavesdk IPC interface needs upload method")

    def close(self):
        self.sdk.close()
