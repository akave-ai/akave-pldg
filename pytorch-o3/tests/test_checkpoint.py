"""
Unit tests for O3CheckpointManager.

Uses mocking to test without requiring real O3 connections.
"""

import io
import os
import sys
import unittest
from unittest.mock import MagicMock

# Store original state for cleanup
_original_modules = {}
_original_path = None


def setUpModule():
    """Set up module-level test fixtures."""
    global _original_modules, _original_path

    # Store original state
    _original_modules = dict(sys.modules)
    _original_path = sys.path.copy()

    # Mock akavesdk before importing our modules
    sys.modules['akavesdk'] = MagicMock()
    sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', 'src'))


def tearDownModule():
    """Clean up module-level test fixtures."""
    # Restore original sys.path
    sys.path[:] = _original_path

    # Remove added modules (keep originals)
    for key in list(sys.modules.keys()):
        if key not in _original_modules:
            del sys.modules[key]


# Run setup before imports
setUpModule()

import torch
import torch.nn as nn

from pytorch_o3.checkpoint import O3CheckpointManager


class SimpleModel(nn.Module):
    """Simple model for testing."""

    def __init__(self):
        super().__init__()
        self.fc = nn.Linear(10, 2)

    def forward(self, x):
        return self.fc(x)


class MockFileMeta:
    """Mock FileMeta returned by upload."""

    def __init__(self, cid):
        self.root_cid = cid


class MockFileInfo:
    """Mock file info object."""

    def __init__(self, name):
        self.name = name


class TestO3CheckpointManager(unittest.TestCase):
    """Tests for O3CheckpointManager."""

    def setUp(self):
        """Set up test fixtures."""
        self.mock_client = MagicMock()
        self.bucket_name = "test-bucket"
        self.prefix = "checkpoints/"

        # Storage for uploaded data
        self.uploaded_data = {}

        def mock_upload(bucket, key, data):
            self.uploaded_data[key] = data
            return MockFileMeta(f"cid_{key}")

        def mock_download(bucket, key):
            if key in self.uploaded_data:
                return self.uploaded_data[key]
            raise FileNotFoundError(f"Key not found: {key}")

        def mock_list_objects(bucket, prefix="", limit=1000):
            return [
                MockFileInfo(key)
                for key in self.uploaded_data.keys()
                if key.startswith(prefix)
            ]

        self.mock_client.upload_object.side_effect = mock_upload
        self.mock_client.download_object.side_effect = mock_download
        self.mock_client.list_objects.side_effect = mock_list_objects

    def test_init(self):
        """Test checkpoint manager initialization."""
        mgr = O3CheckpointManager(
            self.mock_client,
            self.bucket_name,
            prefix=self.prefix
        )

        self.assertEqual(mgr.bucket_name, self.bucket_name)
        self.assertEqual(mgr.prefix, self.prefix)
        self.assertIsNone(mgr._last_cid)

    def test_save_checkpoint(self):
        """Test saving a checkpoint."""
        mgr = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        model = SimpleModel()
        state_dict = model.state_dict()

        cid = mgr.save_checkpoint(
            state_dict=state_dict,
            epoch=5,
            metrics={"loss": 0.1, "accuracy": 0.95}
        )

        # Verify CID returned
        self.assertTrue(cid.startswith("cid_"))

        # Verify upload was called twice (checkpoint + metadata)
        self.assertEqual(self.mock_client.upload_object.call_count, 2)

        # Verify both .pt and .json files were uploaded
        keys = list(self.uploaded_data.keys())
        self.assertEqual(len(keys), 2)
        self.assertTrue(any(k.endswith('.pt') for k in keys))
        self.assertTrue(any(k.endswith('.json') for k in keys))

    def test_save_checkpoint_with_optimizer(self):
        """Test saving checkpoint with optimizer state."""
        mgr = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        model = SimpleModel()
        optimizer = torch.optim.Adam(model.parameters())

        cid = mgr.save_checkpoint(
            state_dict=model.state_dict(),
            epoch=10,
            optimizer_state=optimizer.state_dict()
        )

        self.assertIsNotNone(cid)

        # Load and verify optimizer state is included
        pt_key = [k for k in self.uploaded_data.keys() if k.endswith('.pt')][0]
        checkpoint = torch.load(io.BytesIO(self.uploaded_data[pt_key]), weights_only=False)

        self.assertIn("optimizer_state_dict", checkpoint)

    def test_load_checkpoint_by_cid(self):
        """Test loading checkpoint by CID."""
        mgr = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        model = SimpleModel()
        original_state = model.state_dict()

        # Save checkpoint
        cid = mgr.save_checkpoint(state_dict=original_state, epoch=3)

        # Load by CID
        loaded = mgr.load_checkpoint(cid=cid)

        # Verify state_dict matches
        self.assertIn("model_state_dict", loaded)
        for key in original_state.keys():
            self.assertTrue(
                torch.equal(original_state[key], loaded["model_state_dict"][key])
            )

    def test_load_latest_checkpoint(self):
        """Test loading latest checkpoint without CID."""
        mgr = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        model = SimpleModel()

        # Save multiple checkpoints
        mgr.save_checkpoint(state_dict=model.state_dict(), epoch=1)
        mgr.save_checkpoint(state_dict=model.state_dict(), epoch=2)
        mgr.save_checkpoint(state_dict=model.state_dict(), epoch=3)

        # Load latest (should be epoch 3)
        loaded = mgr.load_checkpoint()

        self.assertEqual(loaded["epoch"], 3)

    def test_load_checkpoint_not_found(self):
        """Test loading non-existent checkpoint raises error."""
        mgr = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        with self.assertRaises(FileNotFoundError):
            mgr.load_checkpoint(cid="nonexistent_cid")

    def test_list_checkpoints(self):
        """Test listing all checkpoints."""
        mgr = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        model = SimpleModel()

        # Save multiple checkpoints
        mgr.save_checkpoint(state_dict=model.state_dict(), epoch=1)
        mgr.save_checkpoint(state_dict=model.state_dict(), epoch=5)
        mgr.save_checkpoint(state_dict=model.state_dict(), epoch=3)

        checkpoints = mgr.list_checkpoints()

        # Should be sorted by epoch descending
        self.assertEqual(len(checkpoints), 3)
        self.assertEqual(checkpoints[0]["epoch"], 5)
        self.assertEqual(checkpoints[1]["epoch"], 3)
        self.assertEqual(checkpoints[2]["epoch"], 1)

    def test_get_latest_cid(self):
        """Test getting latest checkpoint CID."""
        mgr = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        # No checkpoints yet
        self.assertIsNone(mgr.get_latest_cid())

        model = SimpleModel()
        mgr.save_checkpoint(state_dict=model.state_dict(), epoch=1)
        cid2 = mgr.save_checkpoint(state_dict=model.state_dict(), epoch=2)

        self.assertEqual(mgr.get_latest_cid(), cid2)

    def test_resume_training(self):
        """Test auto-resume functionality."""
        mgr = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        # Create and save model
        original_model = SimpleModel()
        optimizer = torch.optim.SGD(original_model.parameters(), lr=0.01)

        mgr.save_checkpoint(
            state_dict=original_model.state_dict(),
            epoch=7,
            optimizer_state=optimizer.state_dict()
        )

        # Create fresh model and resume
        new_model = SimpleModel()
        new_optimizer = torch.optim.SGD(new_model.parameters(), lr=0.01)

        epoch = mgr.resume_training(new_model, new_optimizer)

        self.assertEqual(epoch, 8)

        # Verify weights were loaded
        for key in original_model.state_dict().keys():
            self.assertTrue(
                torch.equal(
                    original_model.state_dict()[key],
                    new_model.state_dict()[key]
                )
            )

    def test_resume_training_no_checkpoint(self):
        """Test resume returns 0 when no checkpoint exists."""
        mgr = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        model = SimpleModel()
        epoch = mgr.resume_training(model)

        self.assertEqual(epoch, 0)

    def test_lineage_tracking(self):
        """Test parent_cid is tracked across saves."""
        mgr = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        model = SimpleModel()

        # First checkpoint has no parent
        cid1 = mgr.save_checkpoint(state_dict=model.state_dict(), epoch=1)

        # Second checkpoint should reference first as parent
        mgr.save_checkpoint(state_dict=model.state_dict(), epoch=2)

        checkpoints = mgr.list_checkpoints()
        epoch2_meta = next(c for c in checkpoints if c["epoch"] == 2)

        self.assertEqual(epoch2_meta["parent_cid"], cid1)

    def test_lineage_continuity_across_reinstantiation(self):
        """Test parent_cid is preserved after manager re-instantiation."""
        mgr1 = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        model = SimpleModel()

        # Save first checkpoint
        cid1 = mgr1.save_checkpoint(state_dict=model.state_dict(), epoch=1)

        # Create NEW manager instance (simulates restart)
        mgr2 = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        # Save second checkpoint with new manager
        mgr2.save_checkpoint(state_dict=model.state_dict(), epoch=2)

        # Verify lineage is preserved (epoch 2 should have epoch 1 as parent)
        checkpoints = mgr2.list_checkpoints()
        epoch2_meta = next(c for c in checkpoints if c["epoch"] == 2)

        self.assertEqual(epoch2_meta["parent_cid"], cid1)

    def test_minimum_size_padding(self):
        """Test that small checkpoints are padded to 127 bytes."""
        mgr = O3CheckpointManager(self.mock_client, self.bucket_name, self.prefix)

        # Create minimal state dict
        state_dict = {"tiny": torch.tensor([1.0])}

        mgr.save_checkpoint(state_dict=state_dict, epoch=1)

        # Find the .pt file and verify size >= 127
        pt_key = [k for k in self.uploaded_data.keys() if k.endswith('.pt')][0]
        self.assertGreaterEqual(len(self.uploaded_data[pt_key]), 127)


class TestCIDExtraction(unittest.TestCase):
    """Tests for CID extraction from various file_meta formats."""

    def test_extract_cid_from_object_attribute(self):
        """Test CID extraction from object with root_cid attribute."""
        mgr = O3CheckpointManager(MagicMock(), "bucket", "prefix/")

        class FileMeta:
            root_cid = "bafy123abc"

        cid = mgr._extract_cid(FileMeta())
        self.assertEqual(cid, "bafy123abc")

    def test_extract_cid_from_cid_attribute(self):
        """Test CID extraction from object with cid attribute."""
        mgr = O3CheckpointManager(MagicMock(), "bucket", "prefix/")

        class FileMeta:
            cid = "bafy456def"

        cid = mgr._extract_cid(FileMeta())
        self.assertEqual(cid, "bafy456def")

    def test_extract_cid_from_dict(self):
        """Test CID extraction from dict."""
        mgr = O3CheckpointManager(MagicMock(), "bucket", "prefix/")

        file_meta = {"root_cid": "bafy789ghi"}
        cid = mgr._extract_cid(file_meta)
        self.assertEqual(cid, "bafy789ghi")

    def test_extract_cid_from_string(self):
        """Test CID extraction from string."""
        mgr = O3CheckpointManager(MagicMock(), "bucket", "prefix/")

        cid = mgr._extract_cid("some_string_cid")
        self.assertEqual(cid, "some_string_cid")

    def test_extract_cid_raises_on_missing(self):
        """Test CID extraction raises RuntimeError when CID missing."""
        mgr = O3CheckpointManager(MagicMock(), "bucket", "prefix/")

        # Empty dict
        with self.assertRaises(RuntimeError) as ctx:
            mgr._extract_cid({})
        self.assertIn("missing CID", str(ctx.exception))

        # Empty string
        with self.assertRaises(RuntimeError):
            mgr._extract_cid("")

        # None
        with self.assertRaises(RuntimeError):
            mgr._extract_cid(None)


if __name__ == "__main__":
    unittest.main()
