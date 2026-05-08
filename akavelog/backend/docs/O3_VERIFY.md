# Verifying uploads to Akave O3

The Akave web UI shows **buckets** only. To confirm that log batches were uploaded and to **list or download objects**, use the **AWS CLI** (O3 is S3-compatible).

## 1. Install AWS CLI

- **Linux/macOS**:  
  https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html  
  Or: `pip install awscli`
- **Docker** (no local install):
  ```bash
  docker run --rm -it -e AWS_ACCESS_KEY_ID -e AWS_SECRET_ACCESS_KEY \
    amazon/aws-cli s3 ls s3://akavelog/logs/ --endpoint-url https://o3-rc2.akave.xyz
  ```
  Set the env vars first (see below).

## 2. Set O3 credentials

Use the same values as in your backend `.env`:

```bash
export AWS_ACCESS_KEY_ID="your-O3-access-key"    # AKAVELOG_STORAGE.O3.ACCESS_KEY
export AWS_SECRET_ACCESS_KEY="your-O3-secret-key" # AKAVELOG_STORAGE.O3.SECRET_KEY
```

Or run the verify script from the backend directory; it reads `.env` automatically.

## 3. Verify with the script (recommended)

From the backend directory:

```bash
chmod +x scripts/verify_o3_upload.sh
./scripts/verify_o3_upload.sh
```

This will:

- List objects under `s3://akavelog/logs/default/`
- Download the latest `.json.gz` batch and print its contents (gunzipped)

To list a different prefix:

```bash
./scripts/verify_o3_upload.sh "logs/default/2026/02/17"
```

## 4. Manual AWS CLI commands

Endpoint and bucket (from `.env`):

- Endpoint: `https://o3-rc2.akave.xyz`
- Bucket: `akavelog`

**List all objects in the bucket:**

```bash
aws s3 ls s3://akavelog/ --recursive --endpoint-url https://o3-rc2.akave.xyz
```

**List log batches (default project):**

```bash
aws s3 ls s3://akavelog/logs/default/ --recursive --endpoint-url https://o3-rc2.akave.xyz
```

**Download a specific batch (e.g. the one from server log):**

```bash
aws s3 cp s3://akavelog/logs/default/2026/02/17/56fffff6-7a01-4864-a7aa-c66b45b8a679.json.gz ./batch.json.gz \
  --endpoint-url https://o3-rc2.akave.xyz
gunzip -c batch.json.gz | head -c 1000
```

**Object metadata (e.g. eCID on Akave network):**

```bash
aws s3api head-object --bucket akavelog \
  --key "logs/default/2026/02/17/56fffff6-7a01-4864-a7aa-c66b45b8a679.json.gz" \
  --endpoint-url https://o3-rc2.akave.xyz
```

## Reference

- [Akave O3 – Upload, Download, Delete Objects](https://docs.akave.ai/akave-o3/object-management/upload-download-delete-objects/)
- [Akave CLI / SDK](https://docs.akave.ai/akave-sdk-cli/) (alternative to AWS CLI for bucket/file management)
