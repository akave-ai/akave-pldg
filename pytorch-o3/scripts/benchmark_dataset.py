"""
Benchmark script to compare O3Dataset performance against local datasets.

This script measures:
- Data loading throughput
- Cache hit rates
- Network call reduction
"""

import time
import argparse
from typing import List
import statistics

import torch
from torch.utils.data import DataLoader
from torchvision import datasets, transforms

from pytorch_o3 import O3Client, O3Dataset


def benchmark_dataloader(dataloader: DataLoader, num_batches: int = 100, warmup: int = 10) -> dict:
    """
    Benchmark a DataLoader's throughput.
    
    Args:
        dataloader: DataLoader to benchmark
        num_batches: Number of batches to measure
        warmup: Number of warmup batches
    
    Returns:
        Dictionary with benchmark results
    """
    device = torch.device('cuda' if torch.cuda.is_available() else 'cpu')
    
    # Warmup
    print(f"Warming up with {warmup} batches...")
    for i, batch in enumerate(dataloader):
        if i >= warmup - 1:
            break
    
    # Actual benchmark
    print(f"Benchmarking with {num_batches} batches...")
    batch_times = []
    total_samples = 0
    start_time = time.time()
    
    for i, batch in enumerate(dataloader):
        if i >= num_batches:
            break
        
        batch_start = time.time()
        # Simulate processing (move to device if needed)
        if isinstance(batch, (list, tuple)):
            batch = [b.to(device) if torch.is_tensor(b) else b for b in batch]
        elif torch.is_tensor(batch):
            batch = batch.to(device)
        
        batch_time = time.time() - batch_start
        batch_times.append(batch_time)
        
        if isinstance(batch, (list, tuple)):
            total_samples += len(batch[0]) if len(batch) > 0 else 0
        elif torch.is_tensor(batch):
            total_samples += batch.shape[0] if len(batch.shape) > 0 else 1
    
    total_time = time.time() - start_time
    
    return {
        'total_time': total_time,
        'total_samples': total_samples,
        'samples_per_second': total_samples / total_time if total_time > 0 else 0,
        'batches_per_second': len(batch_times) / total_time if total_time > 0 else 0,
        'mean_batch_time': statistics.mean(batch_times) if batch_times else 0,
        'median_batch_time': statistics.median(batch_times) if batch_times else 0,
        'min_batch_time': min(batch_times) if batch_times else 0,
        'max_batch_time': max(batch_times) if batch_times else 0,
    }


def benchmark_local_dataset(dataset_path: str, batch_size: int = 32, num_workers: int = 4):
    """Benchmark a local torchvision dataset."""
    print(f"\n{'='*60}")
    print("Benchmarking Local Dataset")
    print(f"{'='*60}")
    
    transform = transforms.Compose([
        transforms.ToTensor(),
    ])
    
    # Use CIFAR-10 as example, but can be adapted
    try:
        dataset = datasets.CIFAR10(
            root=dataset_path,
            train=True,
            download=True,
            transform=transform
        )
    except Exception as e:
        print(f"Error loading CIFAR-10: {e}")
        print("Using a dummy dataset instead...")
        # Create a dummy dataset
        class DummyDataset(torch.utils.data.Dataset):
            def __init__(self, size=1000):
                self.size = size
            
            def __len__(self):
                return self.size
            
            def __getitem__(self, idx):
                return torch.randn(3, 32, 32), torch.randint(0, 10, (1,)).item()
        
        dataset = DummyDataset(size=1000)
    
    dataloader = DataLoader(
        dataset,
        batch_size=batch_size,
        shuffle=True,
        num_workers=num_workers,
        pin_memory=torch.cuda.is_available()
    )
    
    results = benchmark_dataloader(dataloader, num_batches=100)
    
    print("Results:")
    print(f"  Total time: {results['total_time']:.2f}s")
    print(f"  Total samples: {results['total_samples']}")
    print(f"  Samples/second: {results['samples_per_second']:.2f}")
    print(f"  Batches/second: {results['batches_per_second']:.2f}")
    print(f"  Mean batch time: {results['mean_batch_time']*1000:.2f}ms")
    print(f"  Median batch time: {results['median_batch_time']*1000:.2f}ms")
    
    return results


def benchmark_o3_dataset(
    bucket_name: str,
    object_keys: List[str],
    batch_size: int = 32,
    num_workers: int = 0,
    chunk_size: int = 1024 * 1024,
    cache_size: int = 100,
    cache_dir: str = None,
    num_epochs: int = 2
):
    """Benchmark O3Dataset with caching."""
    print("\n" + "="*60)
    print("Benchmarking O3Dataset")
    print("="*60)
    
    if not object_keys:
        print("Error: No object keys provided. Cannot benchmark O3Dataset.")
        return None
    
    # Auto-downgrade num_workers on spawn-based platforms
    import multiprocessing
    if num_workers > 0 and multiprocessing.get_start_method() == 'spawn':
        print(f"Warning: num_workers={num_workers} may cause issues on spawn-based platforms.")
        print("Downgrading to num_workers=0 for compatibility.")
        num_workers = 0
    
    client = O3Client()
    
    try:
        # First epoch - cache miss scenario
        print("\nEpoch 1: Cold cache (cache misses expected)")
        dataset = O3Dataset(
            client=client,
            bucket_name=bucket_name,
            object_keys=object_keys,
            chunk_size=chunk_size,
            cache_size=cache_size,
            cache_dir=cache_dir
        )
        
        dataloader = DataLoader(
            dataset,
            batch_size=batch_size,
            shuffle=True,
            num_workers=num_workers,
            pin_memory=torch.cuda.is_available()
        )
        
        num_batches = max(1, min(100, (len(object_keys) + batch_size - 1) // batch_size))
        epoch1_results = benchmark_dataloader(dataloader, num_batches=num_batches)
        cache_stats_epoch1 = dataset.get_cache_stats()
        
        print("Results (Epoch 1):")
        print(f"  Total time: {epoch1_results['total_time']:.2f}s")
        print(f"  Total samples: {epoch1_results['total_samples']}")
        print(f"  Samples/second: {epoch1_results['samples_per_second']:.2f}")
        print(f"  Cache stats: {cache_stats_epoch1}")
        
        # Second epoch - cache hit scenario
        if num_epochs > 1:
            print("\nEpoch 2: Warm cache (cache hits expected)")
            epoch2_results = benchmark_dataloader(dataloader, num_batches=num_batches)
            cache_stats_epoch2 = dataset.get_cache_stats()
            
            print("Results (Epoch 2):")
            print(f"  Total time: {epoch2_results['total_time']:.2f}s")
            print(f"  Total samples: {epoch2_results['total_samples']}")
            print(f"  Samples/second: {epoch2_results['samples_per_second']:.2f}")
            print(f"  Cache stats: {cache_stats_epoch2}")
            
            # Calculate speedup
            if epoch1_results['total_time'] > 0:
                speedup = epoch1_results['total_time'] / epoch2_results['total_time']
                print(f"\nCache speedup: {speedup:.2f}x")
            
            return {
                'epoch1': epoch1_results,
                'epoch2': epoch2_results,
                'cache_stats_epoch1': cache_stats_epoch1,
                'cache_stats_epoch2': cache_stats_epoch2
            }
        
        return {'epoch1': epoch1_results, 'cache_stats_epoch1': cache_stats_epoch1}
    
    finally:
        client.close()


def main():
    parser = argparse.ArgumentParser(description='Benchmark O3Dataset vs local datasets')
    parser.add_argument('--bucket', type=str, help='Bucket name for O3Dataset')
    parser.add_argument('--object-keys', type=str, nargs='+', help='Object keys in bucket')
    parser.add_argument('--object-keys-file', type=str, help='File containing object keys (one per line)')
    parser.add_argument('--local-dataset-path', type=str, default='./data', help='Path for local dataset')
    parser.add_argument('--batch-size', type=int, default=32, help='Batch size')
    parser.add_argument('--num-workers', type=int, default=0, help='Number of DataLoader workers (0 recommended for O3Dataset)')
    parser.add_argument('--chunk-size', type=int, default=1024*1024, help='Chunk size in bytes')
    parser.add_argument('--cache-size', type=int, default=100, help='LRU cache size')
    parser.add_argument('--cache-dir', type=str, help='Directory for disk cache')
    parser.add_argument('--num-epochs', type=int, default=2, help='Number of epochs to run')
    parser.add_argument('--skip-local', action='store_true', help='Skip local dataset benchmark')
    parser.add_argument('--skip-o3', action='store_true', help='Skip O3Dataset benchmark')
    
    args = parser.parse_args()
    
    # Load object keys
    object_keys = []
    if args.object_keys:
        object_keys = args.object_keys
    elif args.object_keys_file:
        with open(args.object_keys_file, 'r') as f:
            object_keys = [line.strip() for line in f if line.strip()]
    
    # Benchmark local dataset
    local_results = None
    if not args.skip_local:
        try:
            local_results = benchmark_local_dataset(
                args.local_dataset_path,
                batch_size=args.batch_size,
                num_workers=args.num_workers
            )
        except Exception as e:
            print(f"Error benchmarking local dataset: {e}")
    
    # Benchmark O3Dataset
    o3_results = None
    if not args.skip_o3:
        if not args.bucket:
            print("Error: --bucket is required for O3Dataset benchmark")
        elif not object_keys:
            print("Error: --object-keys or --object-keys-file is required for O3Dataset benchmark")
        else:
            try:
                o3_results = benchmark_o3_dataset(
                    bucket_name=args.bucket,
                    object_keys=object_keys,
                    batch_size=args.batch_size,
                    num_workers=args.num_workers,
                    chunk_size=args.chunk_size,
                    cache_size=args.cache_size,
                    cache_dir=args.cache_dir,
                    num_epochs=args.num_epochs
                )
            except Exception as e:
                print(f"Error benchmarking O3Dataset: {e}")
                import traceback
                traceback.print_exc()
    
    # Compare results
    if local_results and o3_results:
        print("\n" + "="*60)
        print("Comparison Summary")
        print("="*60)
        
        o3_epoch1 = o3_results.get('epoch1', {})
        o3_epoch2 = o3_results.get('epoch2', {})
        
        print("\nLocal Dataset:")
        print(f"  Samples/second: {local_results['samples_per_second']:.2f}")
        
        print("\nO3Dataset (Epoch 1 - Cold Cache):")
        print(f"  Samples/second: {o3_epoch1.get('samples_per_second', 0):.2f}")
        
        if o3_epoch2:
            print("\nO3Dataset (Epoch 2 - Warm Cache):")
            print(f"  Samples/second: {o3_epoch2.get('samples_per_second', 0):.2f}")
            
            if local_results['samples_per_second'] > 0:
                ratio = o3_epoch2['samples_per_second'] / local_results['samples_per_second']
                print(f"\nO3Dataset (warm) vs Local: {ratio:.2f}x")


if __name__ == '__main__':
    main()
