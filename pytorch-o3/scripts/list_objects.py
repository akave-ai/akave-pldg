"""
Utility script to list objects in an O3 bucket.

This helps discover object keys for use with O3Dataset.
"""

import argparse
import sys
from pytorch_o3 import O3Client


def main():
    parser = argparse.ArgumentParser(description='List objects in an O3 bucket')
    parser.add_argument('bucket', type=str, help='Bucket name')
    parser.add_argument('--prefix', type=str, default='', help='Object key prefix filter')
    parser.add_argument('--limit', type=int, default=1000, help='Maximum number of objects to list')
    parser.add_argument('--output', type=str, help='Output file to save object keys (one per line)')
    
    args = parser.parse_args()
    
    client = O3Client()
    
    try:
        print(f"Listing objects in bucket '{args.bucket}' with prefix '{args.prefix}'...")
        
        objects = client.list_objects(
            bucket_name=args.bucket,
            prefix=args.prefix,
            limit=args.limit
        )
        
        # Extract object keys - IPCFileListItem objects have .name attribute
        object_keys = []
        for obj in objects:
            if isinstance(obj, str):
                object_keys.append(obj)
            elif hasattr(obj, 'name'):  # IPCFileListItem has .name
                object_keys.append(obj.name)
            elif hasattr(obj, 'key'):
                object_keys.append(obj.key)
            elif isinstance(obj, dict):
                object_keys.append(obj.get('name', obj.get('key', str(obj))))
        
        print(f"\nFound {len(object_keys)} objects:")
        for key in object_keys[:20]:  # Show first 20
            print(f"  {key}")
        if len(object_keys) > 20:
            print(f"  ... and {len(object_keys) - 20} more")
        
        # Save to file if requested
        if args.output:
            with open(args.output, 'w') as f:
                for key in object_keys:
                    f.write(f"{key}\n")
            print(f"\nObject keys saved to {args.output}")
            print(f"You can use this file with --object-keys-file in benchmark_dataset.py")
        
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        import traceback
        traceback.print_exc()
        sys.exit(1)
    finally:
        client.close()


if __name__ == '__main__':
    main()
