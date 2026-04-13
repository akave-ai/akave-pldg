# ZKML Workflows on Akave O3: ezkl Artifact Storage, Integration, and Testing

**Status**: Ready to submit the plan
**Contributor**: Patrick-Ehimen
**Date**: February 3, 2026
**Note**: Open for potential collaborators

## Problem Statement
Zero-knowledge machine learning (ZKML) pipelines produce and consume large artifacts across multiple stages (model formats, compiled circuits, proving/verification keys, proofs, witness data, calibration artifacts, etc.). Today these artifacts are typically stored locally or in centralized object stores, which introduces:
- Fragile workflows (local-only artifacts, poor portability)
- High storage and bandwidth costs when iterating
- Hard-to-reproduce builds due to missing/version-mismatched artifacts
- Limited collaboration and sharing of proofs, keys, and intermediate outputs

As ZKML workflows mature, teams need a reliable object storage backend to support **repeatable, shareable, and auditable** pipelines.

## Related Projects & References
- **ezkl** – Zero-knowledge machine learning framework used for proof generation and verification  
  https://github.com/zkonduit/ezkl


## Objective
Build an integration project that validates **Akave O3** as a storage backend for **ezkl-based ZKML workflows**, focusing on:
- Structured artifact storage in Akave O3
- Automated upload/download of pipeline artifacts
- Reproducible and testable workflow execution
- Reference implementations and CI tests demonstrating end-to-end runs

This project’s primary outcome is a reusable reference pattern for using O3 in ZKML pipelines.

## Scope

### In Scope
- Build a ZKML workflow wrapper that:
  - Runs `ezkl` pipelines (selected subset end-to-end)
  - Stores all produced artifacts in Akave O3
  - Fetches required artifacts from Akave O3 to resume or verify flows
- Artifact versioning and metadata tracking
- Automated tests and CI to validate deterministic and repeatable runs
- Example workflows (small models) that can run in a cohort environment

### Out of Scope
- Production-grade orchestration platform
- Large-scale model proving or GPU-accelerated proving
- Full ezkl feature coverage and performance tuning

## Intended Users / ICP
- ZKML researchers and builders
- Protocol teams exploring verifiable inference
- Teams building privacy-preserving ML pipelines
- Open-source contributors experimenting with zk proofs + ML

## What to Showcase
A working pipeline where:
- A model is prepared (or converted) and run through an ezkl workflow
- Intermediate artifacts (circuits/keys/proofs) are stored in Akave O3
- Another machine (or clean environment) can:
  - Fetch the artifacts from O3
  - Verify proofs successfully
  - Resume pipeline steps without re-generating everything

This demonstrates Akave O3 as a collaboration-friendly artifact store for ZKML.

## Technical Approach (High Level)

### Artifact Taxonomy & Storage Layout (Akave O3)
Define a consistent object naming scheme to store workflow artifacts, for example:

- `projects/<project_id>/runs/<run_id>/inputs/...`
- `projects/<project_id>/runs/<run_id>/models/...`
- `projects/<project_id>/runs/<run_id>/circuits/...`
- `projects/<project_id>/runs/<run_id>/keys/...`
- `projects/<project_id>/runs/<run_id>/proofs/...`
- `projects/<project_id>/runs/<run_id>/reports/...`

Each run includes a metadata manifest such as:
- `manifest.json` containing:
  - artifact hashes
  - ezkl version
  - model parameters
  - input schema
  - command sequence used
  - timestamps

### Workflow Runner
Provide a CLI tool or service that:
- Executes selected ezkl commands in sequence
- Uploads outputs to O3 after each stage
- Downloads missing prerequisites from O3 when resuming

Example stages (exact stages depend on chosen ezkl pipeline path):
- Model preparation / conversion
- Circuit setup / compilation
- Key generation
- Proof generation
- Proof verification

### Integration Layer (Akave O3)
- Implement an O3 client wrapper that supports:
  - Upload with retry/backoff
  - Optional multipart upload for large artifacts
  - Hash verification pre/post upload
  - Download with local caching

### Testing Strategy
Automated tests should validate:
1. **Cold-start run**: run pipeline locally and upload all artifacts to O3
2. **Clean-room verification**: in a fresh environment, download artifacts and verify proof
3. **Resume flow**: delete local outputs, restore from O3, continue pipeline from mid-stage
4. **Integrity checks**: ensure hashes match between local and downloaded artifacts

### CI Plan
- Use minimal models / small inputs to keep runtime practical
- CI runs can:
  - Generate artifacts
  - Upload to a test bucket
  - Re-download and verify
  - Cleanup (optional) or keep for inspection

## Expected Deliverables
- `zkml-o3-runner` (CLI or service):
  - Runs ezkl workflow stages
  - Stores and fetches artifacts from Akave O3
- Reference project examples (small models + inputs)
- Structured artifact layout + manifest format spec
- Comprehensive integration tests + CI workflow
- Documentation:
  - Setup instructions
  - How to run locally
  - How to reproduce a run from O3 artifacts
  - How to extend to larger models/workflows

## Success Criteria
- End-to-end ezkl workflow completes with artifacts stored in O3
- Proof verification succeeds in a clean environment using only O3 artifacts
- Workflow can resume mid-way using downloaded artifacts
- Tests pass reliably and are documented well enough for new contributors

## Validation Goals
- Validate Akave O3 as an artifact store for ZKML pipelines
- Provide a reusable workflow pattern for zkML teams
- Demonstrate that O3 enables reproducibility and collaboration for proof-heavy pipelines
- Expand Akave’s ICP alignment into ZKML / verifiable AI use cases
