#!/usr/bin/env python3
"""
Chaos Test for O3 Checkpoint & Recovery

This script validates the checkpoint system by:
1. Creating a model and training for N epochs
2. Saving checkpoint to Akave O3
3. Wiping all local state
4. Restoring from O3 using CID
5. Verifying model weights match exactly
6. Continuing training to prove resume works

Usage:
    python scripts/chaos_test.py --bucket <bucket-name> [--epochs 5]

Requirements:
    - AKAVE_PRIVATE_KEY environment variable set
    - Bucket must exist in O3
"""

import argparse
import copy
import logging
import os
import sys

import torch
import torch.nn as nn

# Add parent to path for local development
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', 'src'))

from pytorch_o3 import O3Client, O3CheckpointManager

logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


class SimpleModel(nn.Module):
    """Simple model for testing checkpoints."""

    def __init__(self, input_size=10, hidden_size=20, output_size=2):
        super().__init__()
        self.fc1 = nn.Linear(input_size, hidden_size)
        self.fc2 = nn.Linear(hidden_size, output_size)

    def forward(self, x):
        x = torch.relu(self.fc1(x))
        return self.fc2(x)


def train_epoch(model, optimizer, epoch):
    """Simulate one training epoch."""
    model.train()
    # Fake training step
    dummy_input = torch.randn(32, 10)
    dummy_target = torch.randint(0, 2, (32,))

    optimizer.zero_grad()
    output = model(dummy_input)
    loss = nn.CrossEntropyLoss()(output, dummy_target)
    loss.backward()
    optimizer.step()

    return loss.item()


def compare_state_dicts(dict1, dict2):
    """Compare two state_dicts for exact equality."""
    if dict1.keys() != dict2.keys():
        return False, "Keys don't match"

    for key in dict1.keys():
        if not torch.equal(dict1[key], dict2[key]):
            return False, f"Tensor mismatch at key: {key}"

    return True, "All tensors match"


def run_chaos_test(bucket_name: str, epochs: int = 5, prefix: str = "chaos_test/"):
    """Run the full chaos test.

    Args:
        bucket_name: O3 bucket to use
        epochs: Number of epochs to train before checkpoint
        prefix: Checkpoint prefix in bucket

    Returns:
        True if test passes, False otherwise
    """
    print("\n" + "=" * 60)
    print("CHAOS TEST: Checkpoint & Recovery Validation")
    print("=" * 60)

    # Phase 1: Initialize and Train
    print("\n[Phase 1] Initializing model and training...")

    client = O3Client()
    ckpt_mgr = O3CheckpointManager(client, bucket_name, prefix=prefix)

    model = SimpleModel()
    optimizer = torch.optim.Adam(model.parameters(), lr=0.001)

    # Train for N epochs
    for epoch in range(1, epochs + 1):
        loss = train_epoch(model, optimizer, epoch)
        print(f"  Epoch {epoch}/{epochs} - Loss: {loss:.4f}")

    # Save the state for later comparison
    original_state_dict = copy.deepcopy(model.state_dict())
    original_optimizer_state = copy.deepcopy(optimizer.state_dict())

    # Phase 2: Save Checkpoint
    print("\n[Phase 2] Saving checkpoint to Akave O3...")

    cid = ckpt_mgr.save_checkpoint(
        state_dict=model.state_dict(),
        epoch=epochs,
        metrics={"loss": loss},
        optimizer_state=optimizer.state_dict(),
    )
    print(f"  Checkpoint saved with CID: {cid}")

    # Phase 3: Wipe Local State
    print("\n[Phase 3] Wiping local state (simulating failure)...")

    # Delete model and optimizer
    del model
    del optimizer
    del ckpt_mgr
    client.close()
    del client

    # Clear any caches
    torch.cuda.empty_cache() if torch.cuda.is_available() else None
    print("  Local state wiped.")

    # Phase 4: Restore from CID
    print("\n[Phase 4] Restoring from Akave O3...")

    client = O3Client()
    ckpt_mgr = O3CheckpointManager(client, bucket_name, prefix=prefix)

    # Create fresh model
    restored_model = SimpleModel()
    restored_optimizer = torch.optim.Adam(restored_model.parameters(), lr=0.001)

    # Load checkpoint by CID
    checkpoint = ckpt_mgr.load_checkpoint(cid=cid)
    restored_model.load_state_dict(checkpoint["model_state_dict"])
    restored_optimizer.load_state_dict(checkpoint["optimizer_state_dict"])

    print(f"  Restored from CID: {cid}")
    print(f"  Restored epoch: {checkpoint['epoch']}")

    # Phase 5: Verify Weights Match
    print("\n[Phase 5] Verifying model weights...")

    match, message = compare_state_dicts(original_state_dict, restored_model.state_dict())

    if match:
        print(f"  ✓ {message}")
    else:
        print(f"  ✗ {message}")
        client.close()
        return False

    # Phase 6: Continue Training
    print("\n[Phase 6] Continuing training (proves resume works)...")

    for epoch in range(epochs + 1, epochs + 3):
        loss = train_epoch(restored_model, restored_optimizer, epoch)
        print(f"  Epoch {epoch} - Loss: {loss:.4f}")

    print("  ✓ Training continued successfully")

    # Phase 7: Test auto-resume
    print("\n[Phase 7] Testing auto-resume functionality...")

    fresh_model = SimpleModel()
    fresh_optimizer = torch.optim.Adam(fresh_model.parameters(), lr=0.001)

    resume_epoch = ckpt_mgr.resume_training(fresh_model, fresh_optimizer)
    print(f"  Auto-resumed from epoch: {resume_epoch}")

    if resume_epoch == epochs:
        print("  ✓ Auto-resume returned correct epoch")
    else:
        print(f"  ✗ Expected epoch {epochs}, got {resume_epoch}")
        client.close()
        return False

    # Cleanup
    client.close()

    print("\n" + "=" * 60)
    print("CHAOS TEST PASSED")
    print("=" * 60)
    print("\nSummary:")
    print(f"  - Trained for {epochs} epochs")
    print(f"  - Saved checkpoint with CID: {cid}")
    print(f"  - Wiped local state completely")
    print(f"  - Restored model from O3 using CID")
    print(f"  - Verified exact weight match")
    print(f"  - Continued training successfully")
    print(f"  - Auto-resume working correctly")

    return True


def main():
    parser = argparse.ArgumentParser(
        description="Chaos test for O3 checkpoint recovery",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__
    )
    parser.add_argument(
        "--bucket",
        type=str,
        required=True,
        help="Akave O3 bucket name"
    )
    parser.add_argument(
        "--epochs",
        type=int,
        default=5,
        help="Number of epochs to train before checkpoint (default: 5)"
    )
    parser.add_argument(
        "--prefix",
        type=str,
        default="chaos_test/",
        help="Checkpoint prefix in bucket (default: chaos_test/)"
    )

    args = parser.parse_args()

    if not os.getenv('AKAVE_PRIVATE_KEY'):
        print("Error: AKAVE_PRIVATE_KEY environment variable not set")
        sys.exit(1)

    try:
        success = run_chaos_test(args.bucket, args.epochs, args.prefix)
        sys.exit(0 if success else 1)
    except Exception as e:
        logger.exception(f"Chaos test failed with error: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
