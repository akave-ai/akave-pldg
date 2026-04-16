"""MNIST training with O3Dataset and O3CheckpointManager."""

import argparse
import io
import logging
import os
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import List, Optional, Tuple

try:
    from dotenv import load_dotenv
except ImportError:
    load_dotenv = None

from akavesdk import SDKError
from tenacity import RetryError
import torch
import torch.nn as nn
import torch.optim as optim
from torch.utils.data import DataLoader

from pytorch_o3 import O3Client, O3Dataset, O3CheckpointManager


logger = logging.getLogger(__name__)


@dataclass
class TrainConfig:
    batch_size: int = 64
    test_batch_size: int = 1000
    epochs: int = 5
    lr: float = 1e-3
    momentum: float = 0.9
    seed: int = 42
    log_interval: int = 50
    num_workers: int = 2


class SimpleMNISTModel(nn.Module):
    def __init__(self) -> None:
        super().__init__()
        self.conv1 = nn.Conv2d(1, 32, 3, 1)
        self.conv2 = nn.Conv2d(32, 64, 3, 1)
        self.dropout1 = nn.Dropout(0.25)
        self.dropout2 = nn.Dropout(0.5)
        self.fc1 = nn.Linear(9216, 128)
        self.fc2 = nn.Linear(128, 10)

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        x = self.conv1(x)
        x = torch.relu(x)
        x = self.conv2(x)
        x = torch.relu(x)
        x = torch.max_pool2d(x, 2)
        x = self.dropout1(x)
        x = torch.flatten(x, 1)
        x = self.fc1(x)
        x = torch.relu(x)
        x = self.dropout2(x)
        x = self.fc2(x)
        return torch.log_softmax(x, dim=1)


def mnist_o3_transform(data: bytes) -> Tuple[torch.Tensor, torch.Tensor]:
    """Deserialize one MNIST sample from O3 (objects produced by this script)."""
    buffer = io.BytesIO(data)
    obj = torch.load(buffer, weights_only=False)  # MNIST payload is our own format

    if isinstance(obj, dict):
        image = obj.get("image")
        label = obj.get("label")
    else:
        image, label = obj

    if not torch.is_tensor(image):
        raise TypeError("Expected image tensor in O3 object")
    if image.ndim == 2:
        image = image.unsqueeze(0)

    image = image.to(dtype=torch.float32) / 255.0
    mean, std = 0.1307, 0.3081
    image = (image - mean) / std

    label_tensor = torch.tensor(int(label), dtype=torch.long)
    return image, label_tensor


def _list_keys(client: O3Client, bucket: str, prefix: str) -> List[str]:
    objects = client.list_objects(bucket_name=bucket, prefix=prefix, limit=0)
    keys: List[str] = []
    for obj in objects:
        if hasattr(obj, "name") and obj.name:
            keys.append(obj.name)
        elif hasattr(obj, "key") and obj.key:
            keys.append(obj.key)
        elif isinstance(obj, dict):
            keys.append(obj.get("name", obj.get("key", str(obj))))
    keys.sort()
    if not keys:
        raise RuntimeError(f"No objects found in bucket '{bucket}' with prefix '{prefix}'")
    return keys


def _filter_corrupt_keys(
    client: O3Client,
    bucket: str,
    keys: List[str],
    delete_corrupt: bool = False,
    scan: bool = True,
) -> List[str]:
    def _delete_o3_object(key: str) -> None:
        if not hasattr(client.ipc, "file_delete"):
            return
        try:
            client.ipc.file_delete(None, bucket, key)
        except Exception as e:  # best-effort delete
            logger.debug("file_delete failed for %s: %s", key, e)

    if not scan:
        logger.debug(
            "Skipping corrupt-key scan; callers can rely on O3Dataset.__getitem__ retry semantics"
        )
        return keys

    good: List[str] = []
    for key in keys:
        raw = client.download_object(bucket, key)
        try:
            mnist_o3_transform(raw)
            good.append(key)
        except EOFError:
            if delete_corrupt:
                _delete_o3_object(key)
        except Exception as e:  # noqa: BLE001
            logger.warning("Deserialize failed %s: %s", key, e)
    return good


def get_o3_data_loaders(
    client: O3Client,
    config: TrainConfig,
    data_bucket: str,
    train_prefix: str,
    test_prefix: str,
    delete_corrupt: bool = False,
) -> Tuple[DataLoader, DataLoader]:
    train_keys = _list_keys(client, data_bucket, train_prefix)
    test_keys = _list_keys(client, data_bucket, test_prefix)

    if delete_corrupt:
        train_keys = _filter_corrupt_keys(client, data_bucket, train_keys, delete_corrupt=True)
        test_keys = _filter_corrupt_keys(client, data_bucket, test_keys, delete_corrupt=True)

    train_dataset = O3Dataset(
        client=client,
        bucket_name=data_bucket,
        object_keys=train_keys,
        transform=mnist_o3_transform,
        chunk_size=1024 * 1024,
        cache_size=200,
        cache_dir=None,
    )

    test_dataset = O3Dataset(
        client=client,
        bucket_name=data_bucket,
        object_keys=test_keys,
        transform=mnist_o3_transform,
        chunk_size=1024 * 1024,
        cache_size=200,
        cache_dir=None,
    )

    train_loader = DataLoader(
        train_dataset,
        batch_size=config.batch_size,
        shuffle=True,
        num_workers=config.num_workers,
        pin_memory=True,
    )
    test_loader = DataLoader(
        test_dataset,
        batch_size=config.test_batch_size,
        shuffle=False,
        num_workers=config.num_workers,
        pin_memory=True,
    )
    return train_loader, test_loader


def upload_mnist_to_o3(
    client: O3Client,
    data_bucket: str,
    train_prefix: str,
    test_prefix: str,
    local_dir: str,
    upload_limit: Optional[int] = None,
) -> None:
    try:
        from torchvision import datasets, transforms
    except ImportError as e:
        raise ImportError(
            "torchvision is required to use --upload-mnist. "
            "Install it via `pip install torchvision` inside your virtualenv."
        ) from e

    buckets = client.list_buckets()
    if data_bucket not in [getattr(b, "name", None) for b in buckets]:
        client.create_bucket(data_bucket)

    transform = transforms.ToTensor()
    train_ds = datasets.MNIST(local_dir, train=True, download=True, transform=transform)
    test_ds = datasets.MNIST(local_dir, train=False, download=True, transform=transform)

    def _upload_split(dataset, prefix: str, split_name: str) -> None:
        try:
            existing_objs = client.list_objects(bucket_name=data_bucket, prefix=prefix, limit=0)
            existing_keys = set()
            for obj in existing_objs:
                if hasattr(obj, "name") and obj.name:
                    existing_keys.add(obj.name)
                elif hasattr(obj, "key") and obj.key:
                    existing_keys.add(obj.key)
        except Exception:
            existing_keys = set()

        uploaded_count = 0
        for idx, (image, label) in enumerate(dataset):
            if upload_limit is not None and idx >= upload_limit:
                break
            img_uint8 = (image * 255.0).to(torch.uint8)
            obj = {"image": img_uint8, "label": int(label)}
            buf = io.BytesIO()
            torch.save(obj, buf)
            data = buf.getvalue()

            key = f"{prefix}{idx:06d}.pt"
            if key in existing_keys:
                uploaded_count += 1
                continue

            uploaded = False
            for attempt in range(5):
                try:
                    client.upload_object(data_bucket, key, data)
                    uploaded = True
                    break
                except SDKError as e:
                    if "file already exists" in str(e):
                        uploaded = True
                        break
                    if any(x in str(e).lower() for x in ("connection", "reset", "aborted", "unavailable")):
                        if attempt < 4:
                            time.sleep(2 ** attempt)
                            continue
                        break
                    raise
                except RetryError:
                    if attempt < 4:
                        time.sleep(2 ** attempt)
                        continue
                    break

            if uploaded:
                uploaded_count += 1

        logger.info("%s: %d samples", split_name, uploaded_count)

    _upload_split(train_ds, train_prefix, "train")
    _upload_split(test_ds, test_prefix, "test")


def train_one_epoch(
    model: nn.Module,
    device: torch.device,
    train_loader: DataLoader,
    optimizer: optim.Optimizer,
    epoch: int,
    config: TrainConfig,
) -> float:
    model.train()
    criterion = nn.NLLLoss()
    running_loss = 0.0

    for _batch_idx, (data, target) in enumerate(train_loader):
        data, target = data.to(device), target.to(device)

        optimizer.zero_grad()
        output = model(data)
        loss = criterion(output, target)
        loss.backward()
        optimizer.step()

        running_loss += loss.item()

        if _batch_idx % config.log_interval == 0:
            logger.info(
                "Epoch %d [%d/%d] loss=%.4f",
                epoch,
                _batch_idx,
                len(train_loader),
                loss.item(),
            )

    return running_loss / max(len(train_loader), 1)


def evaluate(
    model: nn.Module,
    device: torch.device,
    test_loader: DataLoader,
) -> Tuple[float, float]:
    model.eval()
    criterion = nn.NLLLoss(reduction="sum")
    test_loss = 0.0
    correct = 0

    with torch.no_grad():
        for data, target in test_loader:
            data, target = data.to(device), target.to(device)
            output = model(data)
            test_loss += criterion(output, target).item()
            pred = output.argmax(dim=1, keepdim=True)
            correct += pred.eq(target.view_as(pred)).sum().item()

    n = max(len(test_loader.dataset), 1)
    return test_loss / n, 100.0 * correct / n


def _err_text(exc: BaseException) -> str:
    out = []
    while exc:
        out.append(str(exc))
        exc = getattr(exc, "__cause__", None)
    return " ".join(out).lower()


def _drop_epoch_keys(client: O3Client, bucket: str, prefix: str, ep: int) -> None:
    pat = f"epoch_{ep:04d}_"
    try:
        for obj in client.list_objects(bucket_name=bucket, prefix=prefix, limit=0):
            n = getattr(obj, "name", None) or getattr(obj, "key", None)
            if n and pat in n and hasattr(client.ipc, "file_delete"):
                try:
                    client.ipc.file_delete(None, bucket, n)
                except Exception as e:  # best-effort delete
                    logger.debug("file_delete failed for %s: %s", n, e)
    except Exception:  # list/delete best-effort; ignore
        pass


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="MNIST training with Akave O3 dataset + checkpoints"
    )
    parser.add_argument(
        "--o3-data-bucket",
        type=str,
        required=True,
        help="Akave O3 bucket name containing MNIST training/test objects",
    )
    parser.add_argument(
        "--o3-train-prefix",
        type=str,
        default="mnist/train/",
        help="Prefix within the data bucket for training objects (default: mnist/train/)",
    )
    parser.add_argument(
        "--o3-test-prefix",
        type=str,
        default="mnist/test/",
        help="Prefix within the data bucket for test objects (default: mnist/test/)",
    )
    parser.add_argument(
        "--upload-mnist",
        action="store_true",
        default=False,
        help=(
            "If set, download MNIST locally and upload it into the O3 data bucket "
            "under the given train/test prefixes before training."
        ),
    )
    parser.add_argument(
        "--mnist-local-dir",
        type=str,
        default="./data",
        help="Local directory to cache/download MNIST when using --upload-mnist (default: ./data)",
    )
    parser.add_argument(
        "--upload-limit",
        type=int,
        default=None,
        help=(
            "Maximum number of samples per split (train/test) to upload when using "
            "--upload-mnist / --upload-only. Default: upload the full dataset."
        ),
    )
    parser.add_argument(
        "--upload-only",
        action="store_true",
        default=False,
        help=(
            "If set, only upload MNIST to O3 and exit (no training). "
            "Implies --upload-mnist. Do not pass --o3-checkpoint-bucket."
        ),
    )
    parser.add_argument(
        "--o3-checkpoint-bucket",
        type=str,
        default=None,
        help="Akave O3 bucket name for storing checkpoints (required for training, not for --upload-only)",
    )
    parser.add_argument(
        "--delete-corrupt",
        action="store_true",
        default=False,
        help=(
            "Scan train/test objects before building the dataset. If an object cannot be "
            "deserialized (EOFError), skip it and attempt to delete it from O3."
        ),
    )
    parser.add_argument(
        "--o3-prefix",
        type=str,
        default="mnist-checkpoints/",
        help="Key prefix for checkpoints within the bucket (default: mnist-checkpoints/)",
    )
    parser.add_argument(
        "--epochs",
        type=int,
        default=TrainConfig.epochs,
        help="Number of training epochs (default: 5)",
    )
    parser.add_argument(
        "--batch-size",
        type=int,
        default=TrainConfig.batch_size,
        help="Training batch size (default: 64)",
    )
    parser.add_argument(
        "--lr",
        type=float,
        default=TrainConfig.lr,
        help="Learning rate (default: 1e-3)",
    )
    parser.add_argument(
        "--no-cuda",
        action="store_true",
        default=False,
        help="Disable CUDA even if available",
    )
    return parser.parse_args()


def main() -> None:
    if load_dotenv is not None:
        for path in (Path(__file__).resolve().parent.parent / ".env", Path.cwd() / ".env"):
            if path.exists():
                load_dotenv(path)
                break

    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s - %(levelname)s - %(message)s",
    )

    args = parse_args()

    if args.upload_only:
        args.upload_mnist = True

    if args.upload_only and args.o3_checkpoint_bucket:
        raise RuntimeError(
            "Do not pass --o3-checkpoint-bucket when using --upload-only."
        )
    if not args.upload_only and not args.o3_checkpoint_bucket:
        raise RuntimeError(
            "Training requires --o3-checkpoint-bucket."
        )

    if not os.getenv("AKAVE_PRIVATE_KEY"):
        print("Error: AKAVE_PRIVATE_KEY environment variable not set", file=sys.stderr)
        sys.exit(1)

    client = O3Client()
    try:
        if args.upload_mnist:
            upload_mnist_to_o3(
                client=client,
                data_bucket=args.o3_data_bucket,
                train_prefix=args.o3_train_prefix,
                test_prefix=args.o3_test_prefix,
                local_dir=args.mnist_local_dir,
                upload_limit=args.upload_limit,
            )

        if args.upload_only:
            return

        use_cuda = not args.no_cuda and torch.cuda.is_available()
        device = torch.device("cuda" if use_cuda else "cpu")

        config = TrainConfig(
            batch_size=args.batch_size,
            epochs=args.epochs,
            lr=args.lr,
        )

        torch.manual_seed(config.seed)
        if use_cuda:
            torch.cuda.manual_seed_all(config.seed)

        train_loader, test_loader = get_o3_data_loaders(
            client=client,
            config=config,
            data_bucket=args.o3_data_bucket,
            train_prefix=args.o3_train_prefix,
            test_prefix=args.o3_test_prefix,
            delete_corrupt=args.delete_corrupt,
        )

        model = SimpleMNISTModel().to(device)
        optimizer = optim.SGD(model.parameters(), lr=config.lr, momentum=config.momentum)

        buckets = client.list_buckets()
        if args.o3_checkpoint_bucket not in [getattr(b, "name", None) for b in buckets]:
            client.create_bucket(args.o3_checkpoint_bucket)
        ckpt_manager = O3CheckpointManager(
            client=client,
            bucket_name=args.o3_checkpoint_bucket,
            prefix=args.o3_prefix,
        )

        start_epoch = ckpt_manager.resume_training(model, optimizer)

        for epoch in range(start_epoch, config.epochs):
            train_loss = train_one_epoch(model, device, train_loader, optimizer, epoch, config)
            test_loss, accuracy = evaluate(model, device, test_loader)
            metrics = {
                "train_loss": float(train_loss),
                "test_loss": float(test_loss),
                "accuracy": float(accuracy),
            }

            max_attempts = 5
            for attempt in range(max_attempts):
                try:
                    cid = ckpt_manager.save_checkpoint(
                        model.state_dict(), epoch, metrics=metrics,
                        optimizer_state=optimizer.state_dict(),
                    )
                    logger.info("Epoch %d loss=%.4f acc=%.2f%% cid=%s", epoch, train_loss, accuracy, cid)
                    break
                except Exception as e:
                    txt = _err_text(e)
                    if "file already exists" in txt and attempt < max_attempts - 1:
                        _drop_epoch_keys(client, ckpt_manager.bucket_name, ckpt_manager.prefix, epoch)
                        continue
                    if ("rate" in txt or "resource_exhausted" in txt) and attempt < max_attempts - 1:
                        wait = 120 * (attempt + 1)
                        logger.info("Rate limit; waiting %ds before retry %d/%d", wait, attempt + 1, max_attempts)
                        time.sleep(wait)
                        continue
                    raise

        final_loss, final_acc = evaluate(model, device, test_loader)
        logger.info("Final eval loss=%.4f acc=%.2f%%", final_loss, final_acc)
    finally:
        client.close()


if __name__ == "__main__":
    main()

