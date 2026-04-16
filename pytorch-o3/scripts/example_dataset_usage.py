"""
Example usage of O3Dataset with PyTorch DataLoader.

This script demonstrates how to use O3Dataset for training with PyTorch.
"""

import torch
from torch.utils.data import DataLoader

from pytorch_o3 import O3Client, O3Dataset


def main():
    """Example usage of O3Dataset."""
    
    # Initialize O3Client
    client = O3Client()
    
    # Example: List objects in a bucket to get object keys
    # In practice, you would have a list of object keys you want to use
    bucket_name = "your-bucket-name"
    
    # For demonstration, we'll use placeholder object keys
    # Replace with actual object keys from your bucket
    object_keys = [
        "data/sample1.pt",
        "data/sample2.pt",
        "data/sample3.pt",
        # ... more object keys
    ]
    
    # If you want to list objects from the bucket:
    # buckets = client.list_buckets()
    # bucket = next((b for b in buckets if b.name == bucket_name), None)
    # if bucket:
    #     # Use IPC to list objects - adjust based on actual API
    #     objects = client.ipc.list_objects(None, bucket_name, prefix="data/")
    #     object_keys = [obj.key for obj in objects]
    
    try:
        # Create O3Dataset
        dataset = O3Dataset(
            client=client,
            bucket_name=bucket_name,
            object_keys=object_keys,
            chunk_size=1024 * 1024,  # 1MB chunks
            cache_size=100,  # Cache up to 100 chunks in memory
            cache_dir="./cache",  # Optional: persistent disk cache
        )
        
        # Create DataLoader
        dataloader = DataLoader(
            dataset,
            batch_size=32,
            shuffle=True,
            num_workers=0,
            pin_memory=torch.cuda.is_available()
        )
        
        # Iterate through the dataset
        print("Iterating through dataset...")
        for epoch in range(2):
            print(f"\nEpoch {epoch + 1}")
            for batch_idx, _ in enumerate(dataloader):
                # Your training code here
                # batch contains the data from O3Dataset
                if batch_idx % 10 == 0:
                    print(f"  Batch {batch_idx}, cache stats: {dataset.get_cache_stats()}")
                
                # Example: if batch is a tensor or list of tensors
                # loss = model(batch)
                # ...
        
        # Print final cache statistics
        print("\nFinal cache statistics:")
        print(dataset.get_cache_stats())
        
    finally:
        client.close()


if __name__ == '__main__':
    main()
