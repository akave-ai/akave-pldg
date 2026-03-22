## PyTorch + Akave O3 Integration

This repository provides a set of utilities to use **PyTorch** together with **Akave O3**:

- **`O3Client`**: Thin wrapper around `akavesdk` for range and full-object streaming.
- **`O3Dataset`**: PyTorch `Dataset` that streams sample data from O3 with caching.
- **`O3CheckpointManager`**: Content-addressed (CID-based) checkpoint manager for
  immutable, traceable model snapshots.
- **`examples/train_mnist.py`**: End-to-end MNIST training example using Akave O3
  both for dataset streaming (**`O3Dataset`**) and for checkpoints.

---

## 1. Installation & Environment

### 1.1. Create and activate a virtual environment (required)

Always create a **virtual environment** before installing and running Python code:

```bash
cd /path/to/akave-pytorch-o3

python -m venv .venv
source .venv/bin/activate  # on macOS/Linux
# .venv\Scripts\activate   # on Windows PowerShell

# Copy example environment and customize it
cp .env.example .env
```

### 1.2. Install dependencies

With the virtual environment active:

```bash
pip install --upgrade pip
pip install -r requirements.txt
pip install -e .
```

Core install gives you `O3Client`, `O3Dataset`, and `O3CheckpointManager`.

**To run the MNIST example** (`examples/train_mnist.py`), install the optional extras (python-dotenv, torchvision):

```bash
pip install -e ".[examples]"
```

The core requirements are:

- `torch>=2.0.0`
- `akavesdk` (from the Akave O3 SDK Git repository)
- `tenacity>=8.2.0`

---

## 2. Configuring `AKAVE_PRIVATE_KEY`

Akave O3 authentication relies on the `AKAVE_PRIVATE_KEY` environment variable.
You must set this before using `O3Client`, `O3Dataset`, or `O3CheckpointManager`.

### 2.1. Obtain your Akave private key

Follow the Akave O3 onboarding instructions for your account to generate or
retrieve your private key.

### 2.2. Export the key (macOS/Linux)

```bash
export AKAVE_PRIVATE_KEY="your_hex_or_base64_private_key_here"
```

On Windows PowerShell:

```powershell
$env:AKAVE_PRIVATE_KEY = "your_hex_or_base64_private_key_here"
```

You can verify it is set by running:

```bash
echo "$AKAVE_PRIVATE_KEY"
```

If `AKAVE_PRIVATE_KEY` is missing, `O3Client` will raise an `O3AuthError`
to fail fast with a clear message.

---

## 3. End-to-End MNIST Example (`examples/train_mnist.py`)

The `train_mnist.py` script demonstrates:

- Standard MNIST training loop in PyTorch.
- Storing checkpoints on Akave O3 using `O3CheckpointManager`.
- Automatic resume from the latest checkpoint using CID-based lineage.

### 3.1. Prepare O3 buckets and dataset objects

You will need:

- A **data bucket** that contains MNIST samples as individual objects. (If doesn't exist then code creates one)
- A **checkpoint bucket** where model checkpoints will be stored.

Example bucket names:

- Data bucket: `mnist-data`
- Checkpoint bucket: `mnist-checkpoints`

Within the data bucket, the MNIST samples are expected to be laid out as:

- Training objects under a prefix, e.g. `mnist/train/`
- Test objects under a prefix, e.g. `mnist/test/`

Each object should be a PyTorch-saved tuple or dict:

```python
# Tuple form
torch.save((image_tensor, label_int), f)

# Dict form
torch.save({"image": image_tensor, "label": label_int}, f)
```

Where:

- `image_tensor` has shape `(1, 28, 28)` or `(28, 28)`, dtype `uint8`,
  values in `[0, 255]`.
- `label_int` is the integer class label in `[0, 9]`.

The `examples/train_mnist.py` script loads these bytes from O3, converts images
to `float32`, and applies the standard MNIST normalization.

### 3.2. Run the training script (O3-backed dataset + checkpoints)

With your virtual environment activated and `AKAVE_PRIVATE_KEY` exported:

```bash
cd /path/to/akave-pytorch-o3

python examples/train_mnist.py \
  --o3-data-bucket mnist-data \
  --o3-train-prefix mnist/train/ \
  --o3-test-prefix mnist/test/ \
  --o3-checkpoint-bucket mnist-checkpoints \
  --epochs 5
```

Key arguments:

- `--o3-data-bucket`: **Required.** Name of the Akave O3 bucket that holds
  MNIST training and test objects.
- `--o3-train-prefix`: Prefix within the data bucket for training objects
  (default: `mnist/train/`).
- `--o3-test-prefix`: Prefix within the data bucket for test objects
  (default: `mnist/test/`).
- `--o3-checkpoint-bucket`: **Required.** Name of the Akave O3 bucket to store
  checkpoints in.
- `--o3-prefix`: Optional prefix within the checkpoint bucket (default:
  `mnist-checkpoints/`).
- `--epochs`, `--batch-size`, `--lr`, `--no-cuda`: Standard training controls.

On each epoch, the script:

1. Streams batches of MNIST samples directly from O3 via `O3Dataset`.
2. Trains the model.
3. Evaluates on the test set (also streamed from O3).
4. Saves a checkpoint (`.pt` file) plus a JSON metadata file to the checkpoint
   bucket on O3.
5. Logs the CID (`root_cid`) returned by the O3 upload.

If you re-run the script with the same buckets and prefixes, it will:

- Discover the latest checkpoint via `O3CheckpointManager`.
- Resume training from that epoch (including optimizer state).

**Rate limits:** Large checkpoint uploads can hit O3/node rate limits (e.g. `RESOURCE_EXHAUSTED` or "rate limit wait error ... exceeds limiter's burst" during chunk upload). The example retries up to 5 times with 2, 4, 6, 8 minute backoff when it detects rate-limit errors; on "file already exists" (e.g. after a partial upload) it deletes the orphaned key and retries. If rate limits persist, wait several minutes and re-run the script (it will resume from the last saved checkpoint), or save checkpoints less often (e.g. every N epochs).

---

## 4. API Overview

This section documents the primary high-level APIs exposed by this library.

### 4.1. `O3Client`

Defined in `pytorch_o3.client.O3Client`.

**Purpose**: Light wrapper around `akavesdk` for object listing, range
downloads, and uploads with minimal, PyTorch-friendly semantics.

**Initialization**:

```python
from pytorch_o3 import O3Client

client = O3Client()  # uses AKAVE_PRIVATE_KEY and default IPC address
```

**Key arguments**:

- `private_key`: Optional override for `AKAVE_PRIVATE_KEY`.
- `ipc_address`: IPC endpoint, default `"connect.akave.ai:5500"`.

**Main methods**:

- `list_buckets()`: Return all available buckets.
- `list_objects(bucket_name, prefix="", limit=1000)`: List objects in a bucket.
- `get_object_info(bucket_name, key)`: Inspect object metadata including size.
- `download_object_range(bucket_name, key, start, end)`: Byte range download.
- `download_object(bucket_name, key)`: Full object download.
- `upload_object(bucket_name, key, data: bytes)`: Upload an object, enforcing a
  minimum size (127 bytes); returns a metadata object that includes a CID.
- `close()`: Close underlying SDK resources.

Common error cases:

- Missing `AKAVE_PRIVATE_KEY`: raises `O3AuthError`.
- SDK/IPC misconfiguration: wrapped as `O3AuthError` with the original error
  message.
- Missing SDK capabilities (e.g. no `list_files`): raises `NotImplementedError`.

### 4.2. `O3Dataset`

Defined in `pytorch_o3.dataset.O3Dataset`.

**Purpose**: Stream objects directly from Akave O3 as a PyTorch `Dataset`,
with in-memory LRU and optional on-disk caching.

**Initialization**:

```python
from pytorch_o3 import O3Client, O3Dataset

client = O3Client()
bucket_name = "my-data-bucket"
object_keys = ["data/sample1.pt", "data/sample2.pt"]

dataset = O3Dataset(
    client=client,
    bucket_name=bucket_name,
    object_keys=object_keys,
    chunk_size=1024 * 1024,
    cache_size=100,
    transform=my_bytes_to_tensor_transform,
    cache_dir="/tmp/o3-cache",  # optional
)
```

**Key arguments**:

- `client`: An initialized `O3Client` instance.
- `bucket_name`: Bucket containing your dataset objects.
- `object_keys`: Non-empty list of keys (files) forming the dataset.
- `chunk_size`: Positive integer; logical chunk size for range requests.
- `cache_size`: Non-negative; number of chunks to hold in the in-memory LRU.
- `transform`: Callable converting raw `bytes` into a sample (e.g. tensor, dict).
- `target_transform`: Optional callable to create targets from transformed data.
- `cache_dir`: Optional directory for persistent on-disk chunk cache.

**Worker-safety**:

- Under the hood, each PyTorch worker obtains its own `O3Client` with the same
  credentials and IPC address to avoid cross-process contention.

**Error behaviour**:

- Empty `object_keys` → `ValueError`.
- Non-positive `chunk_size` → `ValueError`.
- Negative `cache_size` → `ValueError`.
- Failures in metadata resolution (e.g. missing size) are wrapped in a
  `RuntimeError` with the offending key.

### 4.3. `O3CheckpointManager`

Defined in `pytorch_o3.checkpoint.O3CheckpointManager`.

**Purpose**: Provide a high-level, CID-based checkpoint interface for PyTorch
models, with lineage tracking and auto-resume.

**Initialization**:

```python
from pytorch_o3 import O3Client
from pytorch_o3.checkpoint import O3CheckpointManager

client = O3Client()
ckpt_mgr = O3CheckpointManager(client, bucket_name="mnist-checkpoints")
```

**Core methods**:

- `save_checkpoint(state_dict, epoch, metrics=None, optimizer_state=None, extra_data=None) -> str`  
  Saves a checkpoint `.pt` file plus JSON metadata and returns the CID.

- `load_checkpoint(cid: str | None = None) -> dict`  
  Loads a specific checkpoint by CID or, if `cid is None`, the latest one.

- `list_checkpoints() -> list[dict]`  
  Returns all checkpoint metadata records sorted by epoch (descending).

- `get_latest_metadata() -> dict | None`  
  Returns metadata for the newest checkpoint or `None`.

- `get_latest_cid() -> str | None`  
  Returns the `root_cid` of the latest checkpoint or `None`.

- `resume_training(model, optimizer=None) -> int`  
  Loads the latest checkpoint into a model (and optionally optimizer) and
  returns the epoch to resume from. If no checkpoint exists, returns `0`.

**Error behaviour**:

- Upload failures propagate via `upload_object` (e.g. if the object is too small
  or connectivity fails).
- If CID cannot be extracted from the upload response, a `RuntimeError` is
  raised with a clear message.
- Malformed metadata files are skipped with a warning; unexpected failures in
  metadata listing are logged with stack traces and then re-raised.

---

## 5. Common Failure Modes & Logging

This section summarizes typical issues and how logging is used to assist
debugging.

- **Missing `AKAVE_PRIVATE_KEY`**  
  - `O3Client` raises `O3AuthError("AKAVE_PRIVATE_KEY is missing.")`.  
  - `examples/train_mnist.py` also performs a pre-check and raises a
    human-readable `RuntimeError` before initializing the SDK.

- **Network or SDK-level errors**  
  - `O3Client` uses `tenacity` retries for `list_buckets`, `download_object`,
    `download_object_range`, and `upload_object` to tolerate transient failures.
  - Persistent errors bubble up so your training script can decide whether to
    retry, abort, or switch behaviour.

- **Malformed or partial checkpoint metadata**  
  - `O3CheckpointManager.list_checkpoints()` logs warnings for JSON decode
    issues and continues processing remaining files.
  - Unexpected exceptions during metadata fetch are logged via
    `logger.exception` and then re-raised.

- **Object size inference problems in `O3Dataset`**  
  - When object metadata does not contain a recognizable size field, a
    `ValueError` is raised with the key name to quickly surface the problem.

- **Rate limits on checkpoint upload**  
  - Uploading large checkpoints can trigger O3/node rate limits (gRPC
    `RESOURCE_EXHAUSTED`, "rate limit" in errors). The MNIST example retries
    with 2–8 minute backoff and, on "file already exists", deletes the partial
    file and retries. If it still fails, wait and re-run; training resumes from
    the latest checkpoint.

For production training, you can integrate these logs into your existing
observability stack (e.g. configure Python logging handlers to send to your
logging backend).

---

## Usage

- Store your own datasets (e.g. preprocessed MNIST or custom tensors) in Akave
  O3 and point `O3Dataset` at those objects for fully decentralized streaming.
- Extend the MNIST example to:
  - Use `O3Dataset` for training data when an O3 bucket is provided.
  - Add richer metrics and experiment metadata in the checkpoint payload.
- Integrate these components into your larger training pipelines or orchestration
  tools.

---

## Contributors

This project was developed by **Team ASBG** over a 3-week sprint:

- **Aries** - Project lead & core implementation
- **Mr_simze** - Milestone contributions & testing
- **Boma_Naps** - Milestone contributions & optimization
- **gerceboss** - Milestone contributions & deployment

We welcome community contributions! Please feel free to open issues, submit pull requests, or reach out with feedback.

