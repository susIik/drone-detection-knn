# AALIS - Acoustic Autonomous Lightweight Interception System

> Advanced real-time acoustic detection and classification system for drone identification using machine learning.

## Overview

AALIS is a production-ready drone acoustic classifier that identifies drone types from audio recordings. The system uses a k-nearest neighbors (KNN) classifier with deep learning embeddings or engineered acoustic features to match incoming audio against a library of known drone signatures.

**Key Features:**
- Real-time audio classification with WebSocket support
- 100% training accuracy on diverse drone datasets
- ~240ms inference latency per sample
- Adaptive confidence thresholding based on signal-to-noise ratio (SNR)
- Extensible prototype library via web interface or CLI
- Comprehensive threat assessment metadata for defense applications

## Architecture

```
┌─────────────────┐
│   Browser UI    │  React + audio recording
└────────┬────────┘
         │ WebSocket/HTTP
         │ (base64 WAV)
         ▼
┌─────────────────┐
│  Go Backend     │
│  ┌───────────┐  │
│  │ Audio     │  │  Preprocessing (filters, AGC, SNR)
│  │ Processing│  │
│  └─────┬─────┘  │
│        │        │
│  ┌─────▼─────┐  │
│  │ Feature   │  │  PANNS embeddings (2048-dim) OR
│  │ Extraction│  │  Legacy features (19-dim)
│  └─────┬─────┘  │
│        │        │
│  ┌─────▼─────┐  │
│  │ Classifier│  │  KNN with adaptive thresholding
│  └─────┬─────┘  │
└────────┼────────┘
         │ JSON response
         ▼
┌─────────────────┐
│  Results Panel  │  Predictions + confidence + metadata
└─────────────────┘
```

## How It Works

### 1. Audio Processing Pipeline

**Client Side:**
- Browser captures 20 seconds of audio (microphone or system audio)
- Audio converted to mono 44.1kHz 16-bit PCM WAV
- Encoded as base64 and sent to server via HTTP POST

**Server Side:**
- Decodes base64 to WAV file
- Applies preprocessing:
  - High-pass filter (>50 Hz)
  - Band-pass filter (100-5000 Hz)
  - Automatic Gain Control (AGC)
  - SNR estimation

### 2. Feature Extraction (Dual Mode)

**PANNS Embeddings (Default):**
- Uses Pretrained Audio Neural Networks
- Generates 2048-dimensional embedding vectors
- Requires Python embedding service (`ml/embedding_service.py`)
- Provides state-of-the-art discrimination

**Legacy Features (Fallback):**
- Extracts 19 hand-crafted acoustic features:
  - Temporal: Energy, ZCR, variance, temporal centroid, onset rate, AM depth
  - Spectral: Centroid, bandwidth, rolloff, flatness, crest, entropy, dominant frequency, skewness, kurtosis, peak prominence
  - Harmonic: Ratio, count, strength
- Used automatically if PANNS service unavailable

### 3. KNN Classification

- Compares input features against prototype library using Euclidean distance
- Selects K nearest neighbors (default K=5)
- Weights neighbors by inverse distance
- Aggregates predictions by label with confidence scores
- Applies adaptive thresholding based on SNR:
  - High SNR (>30 dB): Base threshold (0.55)
  - Moderate SNR (20-30 dB): +0.05 adjustment
  - Low SNR (10-20 dB): +0.10 adjustment
  - Very low SNR (<10 dB): +0.15 adjustment

## Quick Start

### Prerequisites

- **Go 1.21+**: Backend server
- **Node 18+**: Frontend client
- **FFmpeg**: Audio processing
- **Python 3.10+** (optional): For PANNS embeddings

### 1. Install Dependencies

```bash
# macOS
brew install go node ffmpeg

# Ubuntu/Debian
sudo apt install golang nodejs npm ffmpeg

# Windows (using Chocolatey)
choco install golang nodejs ffmpeg
```

### 2. Setup Model

Use existing trained model or train your own:

```bash
# Use example prototypes
cp server/drone/prototypes.example.json server/drone/prototypes.json

# OR train from scratch
cd server
go run ./cmd/train_model \
  -train-dir ../Drone-Training-Data \
  -output drone/prototypes.json
```

### 3. Start Backend

```bash
cd server
go run . serve -proto http -p 5001
```

### 4. Start Frontend

```bash
cd client
npm install
npm start
```

Visit `http://localhost:3000` and grant microphone permissions.

### 5. Optional: Enable PANNS Embeddings

For improved accuracy, start the PANNS embedding service:

```bash
cd ml
pip install -r requirements-panns.txt
python embedding_service.py
```

The Go backend automatically uses PANNS when the service is running on `http://localhost:5002`.

## Usage

### Web Interface

1. Click "Start Listening" to begin recording
2. Select audio source (microphone or system audio)
3. Recording captures 20 seconds automatically
4. View predictions with confidence scores, metadata, and threat assessments
5. Upload new drone samples via the "Upload Prototypes" form

### CLI Tools

**Train Model:**
```bash
cd server
go run ./cmd/train_model -train-dir ../Drone-Training-Data -output drone/prototypes.json
```

**Evaluate Model:**
```bash
go run ./cmd/evaluate_model -model drone/prototypes.json -train-dir ../Drone-Training-Data -k 5
```

**Test Model:**
```bash
go run ./cmd/test_model -model drone/prototypes.json -test-dir "../Test data" -output-csv ../predictions.csv
```

See [`GENERATE_TEST_PREDICTIONS.md`](GENERATE_TEST_PREDICTIONS.md) for detailed testing instructions.

## API Endpoints

### `POST /api/audio/classify`

Classify audio sample.

**Request:**
```json
{
  "audio": "base64_wav_data",
  "channels": 1,
  "sampleRate": 44100,
  "duration": 20.0
}
```

**Response:**
```json
{
  "predictions": [
    {
      "label": "drone_a",
      "category": "drone",
      "confidence": 0.85,
      "averageDistance": 0.12,
      "support": 3,
      "metadata": { "model": "DJI Mavic", "rotor_count": "4" },
      "threatAssessment": {
        "threatLevel": "medium",
        "riskCategory": "surveillance",
        "payloadCapacityKg": 2.5
      }
    }
  ],
  "isDrone": true,
  "latencyMs": 240,
  "snrDb": 25.3,
  "adjustedThreshold": 0.60
}
```

### `POST /api/prototypes/upload`

Upload new prototype samples. Accepts multipart form data with audio files and metadata fields.

See [`DEFENSE_METADATA_FIELDS.md`](DEFENSE_METADATA_FIELDS.md) for complete metadata schema.

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DRONE_MODEL_PATH` | `drone/prototypes.json` | Path to trained model |
| `DRONE_MODEL_K` | `5` | Number of nearest neighbors |
| `USE_PANNS_EMBEDDINGS` | `true` | Enable PANNS embeddings |
| `EMBEDDING_SERVICE_URL` | `http://localhost:5002` | PANNS service URL |
| `DRONE_PERSIST_RECORDINGS` | `true` | Save processed audio |
| `DRONE_RECORDING_DIR` | `frontendrecording` | Recording storage directory |

## ML Pipeline

The complete training and evaluation pipeline is documented in [`ML_PIPELINE.md`](ML_PIPELINE.md).

### Training Data Structure

```
Drone-Training-Data/
├── drone_A/
│   ├── A_01.wav
│   └── A_02.wav
├── drone_B/
│   └── ...
```

Each subdirectory represents a class. Minimum 3-5 samples per class recommended, 10-15+ for best results.

### Automated Pipeline

Run the complete pipeline with one command:

```bash
./pipeline.sh
```

This script:
1. Trains model from `Drone-Training-Data/`
2. Evaluates accuracy with confusion matrix
3. Generates test predictions on `Test data/`

## Performance Metrics

- **Training Accuracy**: 100% (128/128 samples in reference dataset)
- **Inference Speed**: ~240ms per sample
- **Model Size**: 128 prototypes across 10 classes
- **Feature Dimensions**: 2048 (PANNS) or 19 (legacy)

## Project Structure

```
seek-tune/
├── client/              # React frontend
│   └── src/
│       ├── App.js       # Main application
│       ├── components/  # UI components
│       └── pages/       # Page views
├── server/              # Go backend
│   ├── cmd/             # CLI tools (train, evaluate, test)
│   ├── drone/           # Core classifier logic
│   ├── embedding/       # PANNS client
│   ├── wav/             # Audio processing
│   └── main.go          # Server entry point
├── ml/                  # Python ML tools
│   ├── embedding_service.py      # PANNS service
│   ├── build_prototypes.py       # Legacy feature extraction
│   └── rebuild_prototypes_panns.py  # PANNS prototype builder
├── Drone-Training-Data/ # Training dataset
├── pipeline.sh          # Automated ML pipeline
└── README.md            # This file
```

## Documentation

- [`SETUP_GUIDE.md`](SETUP_GUIDE.md) - Detailed setup instructions
- [`ML_PIPELINE.md`](ML_PIPELINE.md) - Complete ML pipeline documentation
- [`PRODUCTION_SUMMARY.md`](PRODUCTION_SUMMARY.md) - Production deployment summary
- [`GENERATE_TEST_PREDICTIONS.md`](GENERATE_TEST_PREDICTIONS.md) - Testing guide
- [`DEFENSE_METADATA_FIELDS.md`](DEFENSE_METADATA_FIELDS.md) - Threat metadata schema

## License

MIT © 2025
