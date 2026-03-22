"""
O3 Checkpoint Manager for PyTorch model state persistence.

Provides decentralized checkpoint storage using Akave O3 with:
- Immutable, content-addressable checkpoints (CID-based)
- Checkpoint lineage tracking (parent CID chain)
- Auto-resume from latest checkpoint
"""

import io
import json
import logging
import uuid
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional, Tuple

import torch

from .client import O3Client

logger = logging.getLogger(__name__)


class O3CheckpointManager:
    """Manages PyTorch model checkpoints on Akave O3 storage.

    Provides save/load functionality with CID-based versioning and
    automatic lineage tracking for reproducible training.

    Example:
        >>> client = O3Client()
        >>> ckpt_mgr = O3CheckpointManager(client, "my-bucket")
        >>> cid = ckpt_mgr.save_checkpoint(model.state_dict(), epoch=10, metrics={"loss": 0.05})
        >>> state_dict = ckpt_mgr.load_checkpoint(cid)
        >>> model.load_state_dict(state_dict)
    """

    def __init__(
        self,
        client: O3Client,
        bucket_name: str,
        prefix: str = "checkpoints/",
    ):
        """Initialize the checkpoint manager.

        Args:
            client: O3Client instance for storage operations
            bucket_name: Bucket to store checkpoints in
            prefix: Key prefix for all checkpoints (default: "checkpoints/")
        """
        self.client = client
        self.bucket_name = bucket_name
        self.prefix = prefix
        self._last_cid: Optional[str] = None

    def save_checkpoint(
        self,
        state_dict: Dict[str, Any],
        epoch: int,
        metrics: Optional[Dict[str, float]] = None,
        optimizer_state: Optional[Dict[str, Any]] = None,
        extra_data: Optional[Dict[str, Any]] = None,
    ) -> str:
        """Save a checkpoint to O3 storage.

        Args:
            state_dict: Model state_dict to save
            epoch: Current training epoch
            metrics: Optional training metrics (loss, accuracy, etc.)
            optimizer_state: Optional optimizer state_dict
            extra_data: Optional additional data to include

        Returns:
            CID (Content Identifier) of the uploaded checkpoint

        Raises:
            RuntimeError: If upload fails
        """
        timestamp = datetime.now(timezone.utc).isoformat()
        uuid_str = uuid.uuid4().hex[:8]
        checkpoint_key = f"{self.prefix}epoch_{epoch:04d}_{timestamp.replace(':', '-')}_{uuid_str}.pt"
        metadata_key = f"{self.prefix}epoch_{epoch:04d}_{timestamp.replace(':', '-')}_{uuid_str}.json"

        # Build checkpoint payload
        checkpoint_data = {
            "epoch": epoch,
            "model_state_dict": state_dict,
        }
        if optimizer_state is not None:
            checkpoint_data["optimizer_state_dict"] = optimizer_state
        if extra_data is not None:
            checkpoint_data.update(extra_data)

        # Serialize checkpoint to bytes
        buffer = io.BytesIO()
        torch.save(checkpoint_data, buffer)
        checkpoint_bytes = buffer.getvalue()

        # Ensure minimum size (pad if necessary)
        if len(checkpoint_bytes) < 127:
            checkpoint_bytes = checkpoint_bytes + b'\x00' * (127 - len(checkpoint_bytes))

        # Upload checkpoint
        logger.info("Uploading checkpoint: %s (%d bytes)", checkpoint_key, len(checkpoint_bytes))
        file_meta = self.client.upload_object(
            self.bucket_name, checkpoint_key, checkpoint_bytes
        )

        # Extract CID from file metadata
        cid = self._extract_cid(file_meta)

        # Determine parent CID for lineage (handles manager re-instantiation)
        parent_cid = self._last_cid
        if parent_cid is None:
            parent_cid = self.get_latest_cid()

        # Build and upload metadata
        metadata = {
            "epoch": epoch,
            "timestamp": timestamp,
            "root_cid": cid,
            "parent_cid": parent_cid,
            "checkpoint_key": checkpoint_key,
            "size_bytes": len(checkpoint_bytes),
            "metrics": metrics or {},
        }

        metadata_bytes = json.dumps(metadata, indent=2).encode('utf-8')
        if len(metadata_bytes) < 127:
            metadata_bytes = metadata_bytes + b' ' * (127 - len(metadata_bytes))

        self.client.upload_object(self.bucket_name, metadata_key, metadata_bytes)
        logger.info("Checkpoint saved: epoch=%s, cid=%s", epoch, cid)

        # Update lineage
        self._last_cid = cid
        return cid

    def load_checkpoint(self, cid: Optional[str] = None) -> Dict[str, Any]:
        """Load a checkpoint from O3 storage.

        Args:
            cid: CID of checkpoint to load. If None, loads the latest checkpoint.

        Returns:
            Checkpoint dictionary containing model_state_dict and other saved data

        Raises:
            FileNotFoundError: If no checkpoint found
            RuntimeError: If download or deserialization fails

        Note:
            Uses weights_only=False for torch.load to support optimizer state.
            Only load checkpoints from trusted sources (your own O3 bucket).
        """
        if cid is None:
            # Load latest checkpoint
            metadata = self.get_latest_metadata()
            if metadata is None:
                raise FileNotFoundError("No checkpoints found in bucket")
            checkpoint_key = metadata["checkpoint_key"]
        else:
            # Find checkpoint by CID
            metadata = self._find_metadata_by_cid(cid)
            if metadata is None:
                raise FileNotFoundError(f"No checkpoint found with CID: {cid}")
            checkpoint_key = metadata["checkpoint_key"]

        logger.info("Loading checkpoint: %s", checkpoint_key)
        checkpoint_bytes = self.client.download_object(self.bucket_name, checkpoint_key)

        # Deserialize
        buffer = io.BytesIO(checkpoint_bytes)
        checkpoint_data = torch.load(buffer, weights_only=False)

        return checkpoint_data

    def _parse_checkpoint_key_for_sort(self, metadata: Dict[str, Any]) -> Tuple[int, str, str]:
        """Parse checkpoint_key into (epoch, timestamp, uuid) for deterministic ordering.

        Key format: {prefix}epoch_{epoch:04d}_{timestamp}_{uuid}.pt (currently, this module only
        creates .pt checkpoint keys and .json metadata keys). But in an extendable format
        like {prefix}epoch_{epoch:04d}_{timestamp}_{uuid}.{suffix}.

        Returns (epoch, timestamp_str, uuid_str); on parse failure returns (epoch, "", "").
        """
        key = metadata.get("checkpoint_key") or ""
        epoch = metadata.get("epoch", 0)
        if not key or not isinstance(key, str):
            return (epoch, "", "")
        base = key
        if self.prefix and base.startswith(self.prefix):
            base = base[len(self.prefix) :]
        for suffix in (".json", ".pt"):
            if base.endswith(suffix):
                base = base[: -len(suffix)]
                break
        parts = base.split("_")
        if len(parts) >= 4 and parts[0] == "epoch":
            try:
                epoch = int(parts[1])
                ts = "_".join(parts[2:-1])
                uuid_str = parts[-1]
                return (epoch, ts, uuid_str)
            except (ValueError, IndexError):
                pass
        return (epoch, "", "")

    def list_checkpoints(self) -> List[Dict[str, Any]]:
        """List all checkpoint metadata in the bucket.

        Returns:
            List of metadata dictionaries, sorted by (epoch, timestamp, uuid) descending
            so the latest checkpoint (same epoch or higher) is first.
        """
        files = self.client.list_objects(self.bucket_name, prefix=self.prefix)
        metadata_files = [f for f in files if hasattr(f, 'name') and f.name.endswith('.json')]

        checkpoints = []
        for f in metadata_files:
            try:
                data = self.client.download_object(self.bucket_name, f.name)
                metadata = json.loads(data.decode('utf-8').rstrip('\x00 '))
                checkpoints.append(metadata)
            except (json.JSONDecodeError, UnicodeDecodeError) as e:
                logger.warning("Skipping malformed metadata %s: %s", f.name, e)
            except Exception:
                logger.exception("Failed to fetch metadata %s", f.name)
                raise

        checkpoints.sort(
            key=lambda x: self._parse_checkpoint_key_for_sort(x),
            reverse=True,
        )
        return checkpoints

    def get_latest_metadata(self) -> Optional[Dict[str, Any]]:
        """Get metadata for the latest checkpoint.

        Returns:
            Metadata dictionary or None if no checkpoints exist
        """
        checkpoints = self.list_checkpoints()
        return checkpoints[0] if checkpoints else None

    def get_latest_cid(self) -> Optional[str]:
        """Get the CID of the latest checkpoint.

        Returns:
            CID string or None if no checkpoints exist
        """
        metadata = self.get_latest_metadata()
        return metadata.get("root_cid") if metadata else None

    def resume_training(
        self,
        model: torch.nn.Module,
        optimizer: Optional[torch.optim.Optimizer] = None,
    ) -> int:
        """Resume training from the latest checkpoint.

        Loads model (and optionally optimizer) state from the latest checkpoint.
        Returns the next epoch to run so the training loop does not replay the
        last completed epoch.

        Args:
            model: PyTorch model to load state into
            optimizer: Optional optimizer to restore state

        Returns:
            Next epoch to run (0 if no checkpoint found). Use as start_epoch in
            ``for epoch in range(start_epoch, num_epochs):``.
        """
        try:
            checkpoint = self.load_checkpoint()
            model.load_state_dict(checkpoint["model_state_dict"])

            if optimizer is not None and "optimizer_state_dict" in checkpoint:
                optimizer.load_state_dict(checkpoint["optimizer_state_dict"])

            saved_epoch = checkpoint.get("epoch", 0)
            logger.info("Resumed from checkpoint at epoch %d; next epoch %d", saved_epoch, saved_epoch + 1)
            return saved_epoch + 1

        except FileNotFoundError:
            logger.info("No checkpoint found, starting from scratch")
            return 0

    def _extract_cid(self, file_meta) -> str:
        """Extract CID from file metadata object.

        Args:
            file_meta: FileMeta object returned by upload

        Returns:
            CID string

        Raises:
            RuntimeError: If CID cannot be extracted from response
        """
        if hasattr(file_meta, 'root_cid'):
            return str(file_meta.root_cid)
        elif hasattr(file_meta, 'cid'):
            return str(file_meta.cid)
        elif isinstance(file_meta, dict):
            cid = file_meta.get('root_cid') or file_meta.get('cid')
            if cid:
                return str(cid)
        elif isinstance(file_meta, str) and file_meta:
            return file_meta
        raise RuntimeError("Upload response missing CID (root_cid/cid)")

    def _find_metadata_by_cid(self, cid: str) -> Optional[Dict[str, Any]]:
        """Find checkpoint metadata by CID.

        Args:
            cid: CID to search for

        Returns:
            Metadata dictionary or None
        """
        checkpoints = self.list_checkpoints()
        for metadata in checkpoints:
            if metadata.get("root_cid") == cid:
                return metadata
        return None
