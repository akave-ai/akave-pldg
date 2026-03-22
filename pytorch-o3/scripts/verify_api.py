"""
Script to verify and discover akavesdk API methods.

This script helps identify which methods are actually available in the
akavesdk IPC interface, making it easier to adapt O3Client methods.
"""

import sys
import inspect
from typing import List, Dict, Any

try:
    from akavesdk import SDK, SDKConfig, SDKError
except ImportError:
    print("Error: akavesdk not installed. Install with:")
    print("  pip install git+https://github.com/d4v1d03/akavesdk-py.git")
    sys.exit(1)

from pytorch_o3 import O3Client
from pytorch_o3.exceptions import O3AuthError


def discover_ipc_methods() -> Dict[str, Any]:
    """Discover all methods available on the IPC interface."""
    print("=" * 60)
    print("Discovering akavesdk IPC Methods")
    print("=" * 60)
    
    try:
        # Create a minimal client to inspect IPC
        # Note: This might fail if credentials are required
        client = O3Client()
        ipc = client.ipc
        
        # Get all methods
        methods = {}
        for name in dir(ipc):
            if name.startswith('_'):
                continue
            
            attr = getattr(ipc, name)
            if callable(attr):
                methods[name] = {
                    'name': name,
                    'signature': str(inspect.signature(attr)),
                    'doc': inspect.getdoc(attr) or "No documentation"
                }
        
        print(f"\nFound {len(methods)} methods on IPC interface:\n")
        for method_name, info in sorted(methods.items()):
            print(f"  {method_name}{info['signature']}")
            if info['doc'] and info['doc'] != "No documentation":
                doc_preview = info['doc'].split('\n')[0][:80]
                print(f"    {doc_preview}...")
        
        client.close()
        return methods
    
    except O3AuthError as e:
        print(f"\nAuthentication error (expected if no credentials): {e}")
        print("\nTrying to inspect SDK/IPC classes directly...")
        
        # Try to inspect the SDK/IPC classes directly using inspect module
        try:
            # Try to get IPC class from SDK
            sdk_class = SDK
            ipc_method = getattr(sdk_class, 'ipc', None)
            
            if ipc_method:
                # Try to get return type annotation or inspect the method
                try:
                    # Create a minimal config to instantiate SDK
                    # SDKConfig requires: address, max_concurrency, block_part_size, use_connection_pool, private_key
                    # Use a valid hex format dummy key (64 hex chars)
                    dummy_key = "0" * 64
                    config = SDKConfig(
                        address="dummy",
                        max_concurrency=1,
                        block_part_size=1000,
                        use_connection_pool=False,
                        private_key=dummy_key
                    )
                    sdk = SDK(config)
                    ipc = sdk.ipc()
                    
                    methods = {}
                    for name in dir(ipc):
                        if name.startswith('_'):
                            continue
                        attr = getattr(ipc, name)
                        if callable(attr):
                            try:
                                sig = str(inspect.signature(attr))
                            except:
                                sig = "(...)"
                            methods[name] = {
                                'name': name,
                                'signature': sig,
                            }
                    
                    print(f"\nFound {len(methods)} methods (dummy connection):\n")
                    for method_name, info in sorted(methods.items()):
                        print(f"  {method_name}{info['signature']}")
                    
                    sdk.close()
                    return methods
                except Exception as e2:
                    print(f"Could not instantiate SDK: {e2}")
                    print("\nTrying to inspect class definition...")
                    
                    # Try to inspect the class without instantiation
                    try:
                        # Look for IPC-related classes in akavesdk
                        import akavesdk
                        ipc_classes = [name for name in dir(akavesdk) if 'ipc' in name.lower() or 'IPC' in name]
                        if ipc_classes:
                            print(f"Found IPC-related classes: {ipc_classes}")
                            for cls_name in ipc_classes:
                                cls = getattr(akavesdk, cls_name)
                                if inspect.isclass(cls):
                                    methods = [m for m in dir(cls) if not m.startswith('_') and callable(getattr(cls, m))]
                                    if methods:
                                        print(f"\n  {cls_name} methods:")
                                        for m in sorted(methods)[:10]:  # Show first 10
                                            print(f"    - {m}")
                    except Exception as e3:
                        print(f"Could not inspect classes: {e3}")
            
            # Final fallback
            print("\nNote: Full method discovery requires valid credentials.")
            print("Set AKAVE_PRIVATE_KEY environment variable to see all methods.")
            print("\nYou can also check the akavesdk source code or documentation:")
            print("  https://github.com/d4v1d03/akavesdk-py")
            return {}
        
        except Exception as e2:
            print(f"Could not inspect: {e2}")
            return {}


def test_object_operations(client: O3Client, bucket_name: str, test_key: str = None):
    """Test object operations with actual API."""
    print("\n" + "=" * 60)
    print("Testing Object Operations")
    print("=" * 60)
    
    ipc = client.ipc
    
    # Test 1: List objects
    print("\n1. Testing list_objects...")
    try:
        objects = client.list_objects(bucket_name, prefix="", limit=10)
        print(f"   ✓ Success! Returned: {type(objects)}")
        if isinstance(objects, (list, tuple)) and len(objects) > 0:
            print(f"   Sample object: {objects[0]}")
            # Try to extract key
            obj = objects[0]
            if hasattr(obj, 'key'):
                test_key = obj.key
                print(f"   Using test key: {test_key}")
            elif isinstance(obj, str):
                test_key = obj
                print(f"   Using test key: {test_key}")
    except NotImplementedError as e:
        print(f"   ✗ {e}")
    except Exception as e:
        print(f"   ✗ Error: {e}")
    
        # Test 2: Get object info
        size = None
        if test_key:
            print(f"\n2. Testing get_object_info for '{test_key}'...")
            try:
                info = client.get_object_info(bucket_name, test_key)
                print(f"   ✓ Success! Returned: {type(info)}")
                print(f"   Info attributes: {dir(info)[:10]}...")
                
                # Try to extract size
                if hasattr(info, 'size'):
                    size = info.size
                elif isinstance(info, dict):
                    size = info.get('size')
                    if size is None:
                        size = info.get('Size')
                print(f"   Object size: {size} bytes" if size else "   Could not determine size")
            except NotImplementedError as e:
                print(f"   ✗ {e}")
            except Exception as e:
                print(f"   ✗ Error: {e}")
            
            # Test 3: Download range
            print(f"\n3. Testing download_object_range for '{test_key}'...")
            try:
                if size and size > 0:
                # Download first 1024 bytes
                data = client.download_object_range(bucket_name, test_key, 0, min(1024, size))
                print(f"   ✓ Success! Downloaded {len(data)} bytes")
            else:
                print("   ⚠ Skipping (object size unknown)")
        except NotImplementedError as e:
            print(f"   ✗ {e}")
        except Exception as e:
            print(f"   ✗ Error: {e}")


def suggest_implementation(methods: Dict[str, Any]):
    """Suggest implementation based on discovered methods."""
    print("\n" + "=" * 60)
    print("Implementation Suggestions")
    print("=" * 60)
    
    method_names = set(methods.keys())
    
    # Check for list methods
    list_candidates = ['list_objects', 'list_bucket_objects', 'get_objects', 'list']
    found_list = [m for m in list_candidates if m in method_names]
    if found_list:
        print(f"\n✓ Found list method: {found_list[0]}")
        print(f"  Use: ipc.{found_list[0]}(...)")
    else:
        print("\n✗ No list method found")
        print("  Check for methods containing 'list' or 'object':")
        list_like = [m for m in method_names if 'list' in m.lower() or 'object' in m.lower()]
        if list_like:
            print(f"  Candidates: {list_like}")
    
    # Check for info methods
    info_candidates = ['get_object_info', 'head_object', 'stat_object', 'object_info', 'info']
    found_info = [m for m in info_candidates if m in method_names]
    if found_info:
        print(f"\n✓ Found info method: {found_info[0]}")
        print(f"  Use: ipc.{found_info[0]}(...)")
    else:
        print("\n✗ No info method found")
        print("  Check for methods containing 'info', 'head', 'stat':")
        info_like = [m for m in method_names if any(x in m.lower() for x in ['info', 'head', 'stat'])]
        if info_like:
            print(f"  Candidates: {info_like}")
    
    # Check for range download methods
    range_candidates = [
        'download_object_range', 'get_object_range', 'read_object',
        'download_range', 'read_range', 'get_range'
    ]
    found_range = [m for m in range_candidates if m in method_names]
    if found_range:
        print(f"\n✓ Found range download method: {found_range[0]}")
        print(f"  Use: ipc.{found_range[0]}(...)")
    else:
        print("\n✗ No range download method found")
        print("  Check for methods containing 'range', 'read', 'download':")
        range_like = [m for m in method_names if any(x in m.lower() for x in ['range', 'read', 'download'])]
        if range_like:
            print(f"  Candidates: {range_like}")
        print("  ⚠ Fallback: Will download full object and slice (inefficient)")


def main():
    """Main verification function."""
    import argparse
    
    parser = argparse.ArgumentParser(description='Verify akavesdk API methods')
    parser.add_argument('--bucket', type=str, help='Bucket name for testing (optional)')
    parser.add_argument('--test-key', type=str, help='Object key for testing (optional)')
    
    args = parser.parse_args()
    
    # Step 1: Discover methods
    methods = discover_ipc_methods()
    
    # Step 2: Suggest implementation
    if methods:
        suggest_implementation(methods)
    
    # Step 3: Test with real operations (if credentials available)
    if args.bucket:
        try:
            print("\n" + "=" * 60)
            print("Testing with Real Operations")
            print("=" * 60)
            client = O3Client()
            test_object_operations(client, args.bucket, args.test_key)
            client.close()
        except O3AuthError as e:
            print(f"\n⚠ Cannot test with real operations: {e}")
            print("Set AKAVE_PRIVATE_KEY environment variable to test")
        except Exception as e:
            print(f"\n⚠ Error during testing: {e}")
            import traceback
            traceback.print_exc()
    else:
        print("\n💡 Tip: Use --bucket <name> to test with real operations")
        print("   Example: python scripts/verify_api.py --bucket my-bucket")
    
    print("\n" + "=" * 60)
    print("Next Steps:")
    print("=" * 60)
    print("1. Review the discovered methods above")
    print("2. Update O3Client methods in src/pytorch_o3/client.py")
    print("3. Test with: python scripts/verify_api.py --bucket <bucket-name>")
    print("4. Run tests: python -m pytest tests/test_dataset.py")


if __name__ == '__main__':
    main()
