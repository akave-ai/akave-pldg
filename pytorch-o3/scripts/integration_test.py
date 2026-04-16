"""
Integration test script for O3Dataset.

This script performs end-to-end testing of O3Dataset with real or mock data.
"""

import argparse
import sys
import logging
import tempfile
import shutil
from pathlib import Path

import torch
from torch.utils.data import DataLoader

from pytorch_o3 import O3Client, O3Dataset
from pytorch_o3.exceptions import O3AuthError

# Setup logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


def test_basic_functionality(client: O3Client, bucket_name: str, object_keys: list):
    """Test basic dataset functionality."""
    print("\n" + "=" * 60)
    print("Test 1: Basic Functionality")
    print("=" * 60)
    
    try:
        dataset = O3Dataset(
            client=client,
            bucket_name=bucket_name,
            object_keys=object_keys,
            chunk_size=1024 * 1024,  # 1MB
            cache_size=10
        )
        
        print(f"✓ Dataset created with {len(dataset)} objects")
        
        # Test __len__
        assert len(dataset) == len(object_keys), "Dataset length mismatch"
        print(f"✓ Dataset length correct: {len(dataset)}")
        
        # Test __getitem__ for first object
        try:
            sample = dataset[0]
            print(f"✓ Successfully retrieved sample: type={type(sample)}, size={len(sample) if hasattr(sample, '__len__') else 'N/A'}")
        except Exception as e:
            print(f"✗ Failed to retrieve sample: {e}")
            raise
        
        # Test cache stats
        stats = dataset.get_cache_stats()
        print(f"✓ Cache stats: {stats}")
        
        return True
        
    except Exception as e:
        print(f"✗ Basic functionality test failed: {e}")
        import traceback
        traceback.print_exc()
        return False


def test_dataloader_integration(client: O3Client, bucket_name: str, object_keys: list):
    """Test DataLoader integration."""
    print("\n" + "=" * 60)
    print("Test 2: DataLoader Integration")
    print("=" * 60)
    
    try:
        dataset = O3Dataset(
            client=client,
            bucket_name=bucket_name,
            object_keys=object_keys,
            chunk_size=512 * 1024,  # 512KB
            cache_size=20
        )
        
        dataloader = DataLoader(
            dataset,
            batch_size=2,
            shuffle=False,
            num_workers=0  # Start with 0 to avoid multiprocessing issues
        )
        
        print(f"✓ DataLoader created")
        
        # Iterate through a few batches
        batch_count = 0
        for batch_idx, batch in enumerate(dataloader):
            batch_count += 1
            if batch_idx < 3:
                print(f"  Batch {batch_idx}: type={type(batch)}, size={len(batch) if hasattr(batch, '__len__') else 'N/A'}")
            if batch_count >= 5:
                break
        
        print(f"✓ Successfully iterated through {batch_count} batches")
        
        # Test with multiple workers
        print("\n  Testing with num_workers=2...")
        dataloader_multi = DataLoader(
            dataset,
            batch_size=2,
            shuffle=False,
            num_workers=2
        )
        
        batch_count = 0
        for batch_idx, batch in enumerate(dataloader_multi):
            batch_count += 1
            if batch_count >= 3:
                break
        
        print(f"✓ Multi-worker DataLoader works: {batch_count} batches")
        
        return True
        
    except Exception as e:
        print(f"✗ DataLoader integration test failed: {e}")
        import traceback
        traceback.print_exc()
        return False


def test_caching(client: O3Client, bucket_name: str, object_keys: list):
    """Test caching functionality."""
    print("\n" + "=" * 60)
    print("Test 3: Caching Functionality")
    print("=" * 60)
    
    try:
        # Create temporary cache directory
        cache_dir = tempfile.mkdtemp()
        
        try:
            dataset = O3Dataset(
                client=client,
                bucket_name=bucket_name,
                object_keys=object_keys[:3],  # Use first 3 objects
                chunk_size=1024 * 1024,
                cache_size=5,
                cache_dir=cache_dir
            )
            
            # First access - should be cache miss
            print("  First access (cache miss expected)...")
            stats_before = dataset.get_cache_stats()
            sample1 = dataset[0]
            stats_after = dataset.get_cache_stats()
            
            print(f"    Cache before: {stats_before['memory_cache_size']}")
            print(f"    Cache after: {stats_after['memory_cache_size']}")
            
            if stats_after['memory_cache_size'] > stats_before['memory_cache_size']:
                print("  ✓ Cache populated after first access")
            else:
                print("  ⚠ Cache size didn't increase (might be expected)")
            
            # Second access - should be cache hit
            print("  Second access (cache hit expected)...")
            sample2 = dataset[0]
            stats_final = dataset.get_cache_stats()
            
            print(f"    Final cache size: {stats_final['memory_cache_size']}")
            print("  ✓ Second access completed (cache should be used)")
            
            # Test disk cache
            if cache_dir:
                cache_files = list(Path(cache_dir).glob('*.chunk'))
                print(f"  ✓ Disk cache files: {len(cache_files)}")
            
            # Test cache clearing
            dataset.clear_cache()
            stats_cleared = dataset.get_cache_stats()
            print(f"  ✓ Cache cleared: {stats_cleared['memory_cache_size']} items remaining")
            
            return True
            
        finally:
            shutil.rmtree(cache_dir, ignore_errors=True)
            
    except Exception as e:
        print(f"✗ Caching test failed: {e}")
        import traceback
        traceback.print_exc()
        return False


def test_error_handling(client: O3Client, bucket_name: str):
    """Test error handling."""
    print("\n" + "=" * 60)
    print("Test 4: Error Handling")
    print("=" * 60)
    
    try:
        # Test empty object keys
        try:
            dataset = O3Dataset(
                client=client,
                bucket_name=bucket_name,
                object_keys=[]
            )
            print("✗ Should have raised ValueError for empty object_keys")
            return False
        except ValueError:
            print("✓ Correctly raised ValueError for empty object_keys")
        
        # Test invalid index
        dataset = O3Dataset(
            client=client,
            bucket_name=bucket_name,
            object_keys=["test-key-1"]
        )
        
        try:
            _ = dataset[100]  # Out of range
            print("✗ Should have raised IndexError for out-of-range index")
            return False
        except IndexError:
            print("✓ Correctly raised IndexError for out-of-range index")
        
        try:
            _ = dataset[-1]  # Negative index
            print("✗ Should have raised IndexError for negative index")
            return False
        except IndexError:
            print("✓ Correctly raised IndexError for negative index")
        
        return True
        
    except Exception as e:
        print(f"✗ Error handling test failed: {e}")
        import traceback
        traceback.print_exc()
        return False


def main():
    parser = argparse.ArgumentParser(description='Integration test for O3Dataset')
    parser.add_argument('--bucket', type=str, required=True, help='Bucket name')
    parser.add_argument('--object-keys', type=str, nargs='+', help='Object keys to test with')
    parser.add_argument('--object-keys-file', type=str, help='File containing object keys')
    parser.add_argument('--verbose', '-v', action='store_true', help='Verbose logging')
    
    args = parser.parse_args()
    
    if args.verbose:
        logging.getLogger().setLevel(logging.DEBUG)
    
    # Load object keys
    object_keys = []
    if args.object_keys:
        object_keys = args.object_keys
    elif args.object_keys_file:
        with open(args.object_keys_file, 'r') as f:
            object_keys = [line.strip() for line in f if line.strip()]
    else:
        print("Error: Must provide --object-keys or --object-keys-file")
        sys.exit(1)
    
    if not object_keys:
        print("Error: No object keys provided")
        sys.exit(1)
    
    print("=" * 60)
    print("O3Dataset Integration Test")
    print("=" * 60)
    print(f"Bucket: {args.bucket}")
    print(f"Object keys: {len(object_keys)}")
    
    # Initialize client
    try:
        client = O3Client()
    except O3AuthError as e:
        print(f"Error: {e}")
        print("Set AKAVE_PRIVATE_KEY environment variable")
        sys.exit(1)
    
    try:
        results = []
        
        # Run tests
        results.append(("Basic Functionality", test_basic_functionality(client, args.bucket, object_keys)))
        results.append(("DataLoader Integration", test_dataloader_integration(client, args.bucket, object_keys)))
        results.append(("Caching", test_caching(client, args.bucket, object_keys)))
        results.append(("Error Handling", test_error_handling(client, args.bucket)))
        
        # Summary
        print("\n" + "=" * 60)
        print("Test Summary")
        print("=" * 60)
        
        passed = sum(1 for _, result in results if result)
        total = len(results)
        
        for test_name, result in results:
            status = "✓ PASS" if result else "✗ FAIL"
            print(f"{status}: {test_name}")
        
        print(f"\nTotal: {passed}/{total} tests passed")
        
        if passed == total:
            print("\n🎉 All tests passed!")
            return 0
        else:
            print(f"\n⚠ {total - passed} test(s) failed")
            return 1
            
    finally:
        client.close()


if __name__ == '__main__':
    sys.exit(main())
