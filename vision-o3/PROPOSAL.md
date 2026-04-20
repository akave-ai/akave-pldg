# Vision-O3: Edge-AI Model Registry & Verifiable CV Datasets

## Problem Statement
Edge devices (like Raspberry Pi) often pull model weights (YOLO, ONNX) from centralized hubs like HuggingFace or AWS S3, creating single points of failure and vendor lock-in. Additionally, training datasets for specialized computer vision tasks are large, difficult to version, and challenging to verify across decentralized teams. As edge AI deployments scale, there is a growing need for:
- Decentralized, verifiable model distribution
- Content-addressed storage for model artifacts
- Immutable dataset versioning with cryptographic verification
- Lightweight edge device integration without infrastructure overhead

## Objective
Build a **decentralized edge-AI model registry and verifiable computer vision dataset management system** where:
- **Model artifacts** (.pt, .onnx, .tflite, .h5) are stored immutably on **Akave O3**
- **Training datasets** are versioned and content-addressed for reproducibility
- **Edge devices** fetch authenticated, CID-verified model updates via a lightweight FastAPI gateway
- **ML engineers** can collaborate on dataset curation without managing large files in version control

This project validates **Akave O3** as a backend for high-bandwidth binary artifacts and "warm" data use cases for AI training and edge deployment.

## Scope

### In Scope
- Model artifact storage and versioning (.pt, .onnx, .tflite, .h5)
- Training dataset storage with manifest generation
- FastAPI gateway for authenticated access
- CID-based verification for model integrity
- Python Client SDK for edge devices
- Role-based access control and authentication
- Version history and metadata management
- Docker containerization for easy deployment

### Out of Scope
- Model training infrastructure
- Model serving / inference infrastructure
- Large-scale distributed training orchestration
- Advanced RBAC or enterprise SSO integration
- Real-time model updates or streaming

## Intended Users / ICP
- Edge AI developers deploying models to Raspberry Pi and IoT devices
- Machine learning engineers working on computer vision tasks
- Open-source ML projects requiring decentralized model distribution
- Blockchain and Web3 projects building verifiable AI systems
- Cost-conscious ML teams avoiding vendor lock-in
- Research teams requiring reproducible dataset versioning

## High-Level Architecture

### FastAPI Gateway
- Lightweight REST API for model and dataset operations
- JWT-based authentication with role-based permissions
- Metadata management (separate from artifacts)
- Integration with Akave O3 for storage operations
- OpenAPI documentation and health checks

### Akave O3 Storage Backend
- Decentralized, content-addressed storage for immutable artifacts
- CID-based verification for model integrity
- Bucket organization for models and datasets
- Support for large file streaming

### Python Client SDK
- Lightweight library for edge devices
- Automatic CID verification on download
- Local caching with LRU policy
- Retry logic with exponential backoff
- Methods for listing, downloading, and verifying artifacts

### Metadata Store
- SQLite or PostgreSQL for queryable metadata
- Tracks model versions, dataset versions, and user permissions
- Enables fast discovery without duplicating artifacts
- Supports filtering, pagination, and search

## Expected Deliverables
- FastAPI Gateway service with full REST API
- Python Client SDK for edge devices
- Akave O3 integration layer
- Metadata store with versioning support
- Docker configuration for deployment
- Example scripts demonstrating:
  - Model upload and versioning
  - Edge device model fetching with verification
  - Dataset management and manifest generation
- Comprehensive documentation covering:
  - Setup and configuration
  - API endpoint usage
  - SDK integration examples
  - Akave O3 credential setup

## Success Criteria
- Models and datasets reliably stored and retrieved from Akave O3
- Edge devices can fetch and verify models using CID validation
- Version history and metadata management working end-to-end
- Authentication and access control enforced across all operations
- SDK provides drop-in integration for edge applications
- Clear performance comparison with centralized alternatives
- Reusable patterns for other ML projects adopting Akave

## Validation Goals
- Prove Akave O3 is suitable for edge-AI model distribution
- Demonstrate decentralized storage for reproducible ML workflows
- Showcase content-addressed verification for model integrity
- Validate Akave O3 for high-bandwidth binary artifacts
- Provide reference architecture for edge AI teams
