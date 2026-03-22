import sys
from akavesdk import SDKError
from pytorch_o3.client import O3Client

BUCKET_NAME = "test-bucket"

def main():
    client = O3Client()
    try:
        print("Creating bucket")
        try:
            client.create_bucket(BUCKET_NAME)
            print("Bucket created")
        except SDKError as e:
            print("Bucket may already exist", e)

        print("Listing buckets")
        buckets = client.list_buckets()
        print("Buckets:", buckets)

        bucket_names=[b.name for b in buckets]
        if BUCKET_NAME in bucket_names:
            print("Verification successful")
        else:
            print("Verification failed")
            sys.exit(1)

    finally:
        client.close()


if __name__ == "__main__":
    main()