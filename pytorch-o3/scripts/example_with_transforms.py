"""
Example showing O3Dataset with transforms for different data formats.

This demonstrates how to use transforms to handle various data formats
like PyTorch tensors, images, JSON, etc.
"""

import io
import torch
from torch.utils.data import DataLoader

from pytorch_o3 import O3Client, O3Dataset


def torch_tensor_transform(data: bytes) -> torch.Tensor:
    """Transform bytes to PyTorch tensor."""
    buffer = io.BytesIO(data)
    return torch.load(buffer, weights_only=True)


def image_transform(data: bytes) -> torch.Tensor:
    """Transform bytes to PyTorch tensor from image."""
    try:
        from PIL import Image
        import torchvision.transforms as transforms
    except ImportError:
        raise ImportError("PIL and torchvision are required for image transforms")
    image = Image.open(io.BytesIO(data))
    transform = transforms.Compose([
        transforms.ToTensor(),
    ])
    return transform(image)


def json_transform(data: bytes) -> dict:
    """Transform bytes to JSON dict."""
    import json
    return json.loads(data.decode('utf-8'))


def main():
    """Example usage with different transforms."""
    
    client = O3Client()
    bucket_name = "your-bucket-name"
    
    # Example 1: PyTorch tensor files (.pt, .pth)
    print("Example 1: Loading PyTorch tensor files")
    tensor_keys = [
        "data/tensor1.pt",
        "data/tensor2.pt",
    ]
    
    tensor_dataset = O3Dataset(
        client=client,
        bucket_name=bucket_name,
        object_keys=tensor_keys,
        transform=torch_tensor_transform,
        chunk_size=1024 * 1024,
        cache_size=50
    )
    
    tensor_loader = DataLoader(tensor_dataset, batch_size=2, num_workers=0)
    for batch in tensor_loader:
        print(f"Tensor batch shape: {batch.shape if torch.is_tensor(batch) else 'varies'}")
        break  # Just show first batch
    
    # Example 2: Image files
    print("\nExample 2: Loading image files")
    image_keys = [
        "data/image1.jpg",
        "data/image2.png",
    ]
    
    image_dataset = O3Dataset(
        client=client,
        bucket_name=bucket_name,
        object_keys=image_keys,
        transform=image_transform,
        chunk_size=512 * 1024,  # Smaller chunks for images
        cache_size=100
    )
    
    image_loader = DataLoader(image_dataset, batch_size=4, num_workers=0)
    for batch in image_loader:
        print(f"Image batch shape: {batch.shape if torch.is_tensor(batch) else 'varies'}")
        break
    
    # Example 3: JSON files
    print("\nExample 3: Loading JSON files")
    json_keys = [
        "data/config1.json",
        "data/config2.json",
    ]
    
    json_dataset = O3Dataset(
        client=client,
        bucket_name=bucket_name,
        object_keys=json_keys,
        transform=json_transform,
        chunk_size=64 * 1024,  # Small chunks for JSON
        cache_size=200
    )
    
    json_loader = DataLoader(json_dataset, batch_size=8, num_workers=0)
    for batch in json_loader:
        print(f"JSON batch: {type(batch)}")
        break
    
    # Example 4: Custom transform with error handling
    def safe_torch_load(data: bytes) -> torch.Tensor:
        """Safely load tensor with error handling."""
        try:
            buffer = io.BytesIO(data)
            return torch.load(buffer, weights_only=True)
        except Exception as e:
            print(f"Error loading tensor: {e}")
            return torch.zeros(1)
    
    print("\nExample 4: Custom transform with error handling")
    O3Dataset(
        client=client,
        bucket_name=bucket_name,
        object_keys=tensor_keys,
        transform=safe_torch_load,
    )
    
    client.close()


if __name__ == '__main__':
    main()
