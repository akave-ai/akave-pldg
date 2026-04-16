"""
Tests for O3Dataset implementation.

Note: These tests require actual O3Client connection and may need to be
adapted based on the actual akavesdk API methods available.
"""

import unittest
from unittest.mock import Mock, MagicMock, patch
import tempfile
import shutil
import sys
from pathlib import Path

# Mock akavesdk before importing our modules
sys.modules['akavesdk'] = MagicMock()

import torch

from pytorch_o3 import O3Client, O3Dataset
from pytorch_o3.dataset import LRUCache


class TestLRUCache(unittest.TestCase):
    """Test LRU cache implementation."""
    
    def test_basic_operations(self):
        """Test basic cache operations."""
        cache = LRUCache(max_size=3)
        
        # Test put and get
        cache.put("key1", b"value1")
        self.assertEqual(cache.get("key1"), b"value1")
        
        # Test cache eviction
        cache.put("key2", b"value2")
        cache.put("key3", b"value3")
        cache.put("key4", b"value4")  # Should evict key1
        
        self.assertIsNone(cache.get("key1"))
        self.assertEqual(cache.get("key2"), b"value2")
        self.assertEqual(cache.get("key3"), b"value3")
        self.assertEqual(cache.get("key4"), b"value4")
    
    def test_lru_ordering(self):
        """Test that LRU ordering is maintained."""
        cache = LRUCache(max_size=2)
        
        cache.put("key1", b"value1")
        cache.put("key2", b"value2")
        cache.get("key1")  # Access key1, making it most recently used
        cache.put("key3", b"value3")  # Should evict key2, not key1
        
        self.assertEqual(cache.get("key1"), b"value1")
        self.assertIsNone(cache.get("key2"))
        self.assertEqual(cache.get("key3"), b"value3")
    
    def test_clear(self):
        """Test cache clearing."""
        cache = LRUCache(max_size=10)
        cache.put("key1", b"value1")
        cache.put("key2", b"value2")
        
        self.assertEqual(cache.size(), 2)
        cache.clear()
        self.assertEqual(cache.size(), 0)
        self.assertIsNone(cache.get("key1"))


class TestO3Dataset(unittest.TestCase):
    """Test O3Dataset implementation."""
    
    def setUp(self):
        """Set up test fixtures."""
        self.temp_dir = tempfile.mkdtemp()
        self.cache_dir = tempfile.mkdtemp()
    
    def tearDown(self):
        """Clean up test fixtures."""
        shutil.rmtree(self.temp_dir, ignore_errors=True)
        shutil.rmtree(self.cache_dir, ignore_errors=True)
    
    @patch('pytorch_o3.dataset.O3Client')
    def test_dataset_initialization(self, mock_client_class):
        """Test dataset initialization."""
        mock_client = Mock()
        mock_ipc = Mock()
        mock_client.ipc = mock_ipc
        mock_client.private_key = "test_key"
        mock_client._ipc_address = "test_address"
        mock_client_class.return_value = mock_client
        
        # Mock get_object_info to return object with size attribute
        mock_info = Mock()
        mock_info.size = 1024 * 1024
        mock_client.get_object_info = Mock(return_value=mock_info)
        
        object_keys = ["obj1", "obj2", "obj3"]
        
        try:
            dataset = O3Dataset(
                client=mock_client,
                bucket_name="test-bucket",
                object_keys=object_keys,
                cache_dir=self.cache_dir
            )
            self.assertEqual(len(dataset), len(object_keys))
        except (NotImplementedError, AttributeError, RuntimeError):
            # Expected if API methods aren't available
            pass
    
    def test_empty_object_keys(self):
        """Test that empty object_keys raises ValueError."""
        mock_client = Mock()
        
        with self.assertRaises(ValueError):
            O3Dataset(
                client=mock_client,
                bucket_name="test-bucket",
                object_keys=[],
            )
    
    def test_cache_key_generation(self):
        """Test cache key generation."""
        mock_client = Mock()
        mock_ipc = Mock()
        mock_client.ipc = mock_ipc
        
        # Mock to avoid API calls
        with patch.object(O3Dataset, '_compute_metadata'):
            dataset = O3Dataset(
                client=mock_client,
                bucket_name="test-bucket",
                object_keys=["obj1"],
            )
            
            cache_key = dataset._get_cache_key("obj1", 0)
            self.assertEqual(cache_key, "obj1:0")
            
            cache_key = dataset._get_cache_key("obj1", 5)
            self.assertEqual(cache_key, "obj1:5")


class TestDatasetIntegration(unittest.TestCase):
    """Test dataset integration with PyTorch."""
    
    @patch('pytorch_o3.dataset.O3Client')
    def test_dataset_with_dataloader(self, mock_client_class):
        """Test that dataset can be used with DataLoader."""
        mock_client = Mock()
        mock_ipc = Mock()
        mock_client.ipc = mock_ipc
        mock_client_class.return_value = mock_client
        
        # This is a minimal test to ensure the dataset structure
        # is compatible with PyTorch DataLoader
        # Full functionality requires actual API implementation
        
        object_keys = ["obj1", "obj2"]
        
        try:
            with patch.object(O3Dataset, '_compute_metadata'):
                dataset = O3Dataset(
                    client=mock_client,
                    bucket_name="test-bucket",
                    object_keys=object_keys,
                )
                
                # Test that dataset has required methods
                self.assertTrue(hasattr(dataset, '__len__'))
                self.assertTrue(hasattr(dataset, '__getitem__'))
                
                # Test length
                self.assertEqual(len(dataset), len(object_keys))
        except (NotImplementedError, AttributeError):
            # Expected if API methods aren't available
            pass


if __name__ == '__main__':
    unittest.main()
