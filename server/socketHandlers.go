package main

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"strconv"
	"time"

	"song-recognition/detections"
	"song-recognition/drone"
	"song-recognition/embedding"
	"song-recognition/models"
	"song-recognition/utils"

	socketio "github.com/googollee/go-socket.io"
	"github.com/mdobak/go-xerrors"
)

type socketController struct {
	classifier        *drone.Classifier
	templateMatcher   *drone.TemplateMatcher
	persistRecordings bool
}

const (
	socketSlidingWindowDurationSeconds  = 3.0
	socketSlidingWindowOverlapSeconds   = 1.5
	socketMinSlidingAnalysisDurationSec = 4.0
)

func newSocketController(classifier *drone.Classifier, matcher *drone.TemplateMatcher, persist bool) *socketController {
	return &socketController{classifier: classifier, templateMatcher: matcher, persistRecordings: persist}
}

func (c *socketController) emitModelInfo(socket socketio.Conn) {
	stats := c.classifier.Stats()
	socket.Emit("modelInfo", stats)
}

func (c *socketController) handleRequestModelInfo(socket socketio.Conn) {
	c.emitModelInfo(socket)
}

func (c *socketController) handleNewRecording(socket socketio.Conn, recordData string) {
	logger := utils.GetLogger()
	ctx := context.Background()

	log.Printf("[handleNewRecording] Starting for socket %s, data length: %d\n", socket.ID(), len(recordData))
	logger.InfoContext(ctx, "handleNewRecording called",
		slog.String("socketID", socket.ID()),
		slog.Int("dataLength", len(recordData)),
	)

	if recordData == "" {
		logger.ErrorContext(ctx, "no data received in newRecording event")
		socket.Emit("analysisError", map[string]string{"message": "no audio data received"})
		return
	}

	var recData models.RecordData
	if err := json.Unmarshal([]byte(recordData), &recData); err != nil {
		err := xerrors.New(err)
		logger.ErrorContext(ctx, "failed to parse record payload", slog.Any("error", err))
		socket.Emit("analysisError", map[string]string{"message": "invalid audio payload"})
		return
	}

	log.Printf("[handleNewRecording] Parsed recording data: sampleRate=%d, channels=%d, duration=%.2f\n",
		recData.SampleRate, recData.Channels, recData.Duration)
	logger.InfoContext(ctx, "received recording",
		slog.String("socketID", socket.ID()),
		slog.Int("sampleRate", recData.SampleRate),
		slog.Int("channels", recData.Channels),
		slog.Int("sampleSize", recData.SampleSize),
		slog.Float64("duration", recData.Duration),
	)

	started := time.Now()

	log.Printf("[handleNewRecording] Preparing audio sample for socket %s\n", socket.ID())
	audioSample, err := drone.PrepareAudioSample(recData, c.persistRecordings)
	if err != nil {
		err := xerrors.New(err)
		logger.ErrorContext(ctx, "failed to prepare audio sample", slog.Any("error", err))
		socket.Emit("analysisError", map[string]string{"message": "unable to decode audio"})
		return
	}

	logger.InfoContext(ctx, "prepared audio sample",
		slog.String("socketID", socket.ID()),
		slog.Int("sampleRate", audioSample.SampleRate),
		slog.Int("frameCount", len(audioSample.Samples)),
		slog.Float64("duration", audioSample.Duration),
		slog.Bool("persisted", audioSample.Persisted != ""),
	)

	// Extract features - use PANNS if available and prototypes are PANNS-based
	var features []float64
	usePANNS := utils.GetEnv("USE_PANNS_EMBEDDINGS", "true") == "true"

	if usePANNS && audioSample.Persisted != "" {
		// Use PANNS embedding service
		embeddingServiceURL := utils.GetEnv("EMBEDDING_SERVICE_URL", "http://panns:5002")
		pannsClient := embedding.NewPANNSClient(embeddingServiceURL)

		// Call PANNS service to get embedding
		embeddingVec, err := pannsClient.EmbedFile(audioSample.Persisted)
		if err != nil {
			logger.WarnContext(ctx, "PANNS embedding failed, falling back to legacy features",
				slog.String("socketID", socket.ID()),
				slog.Any("error", err))
			// Fall back to old feature extraction
			features, err = drone.ExtractFeatureVector(audioSample.Samples, audioSample.SampleRate)
			if err != nil {
				err := xerrors.New(err)
				logger.ErrorContext(ctx, "failed to extract features", slog.Any("error", err))
				socket.Emit("analysisError", map[string]string{"message": "unable to extract features"})
				return
			}
		} else {
			features = embeddingVec
			logger.InfoContext(ctx, "extracted PANNS embedding",
				slog.String("socketID", socket.ID()),
				slog.Int("dimension", len(features)),
			)
		}
	} else {
		// Use legacy feature extraction
		features, err = drone.ExtractFeatureVector(audioSample.Samples, audioSample.SampleRate)
		if err != nil {
			err := xerrors.New(err)
			logger.ErrorContext(ctx, "failed to extract features", slog.Any("error", err))
			socket.Emit("analysisError", map[string]string{"message": "unable to extract features"})
			return
		}
		logger.InfoContext(ctx, "extracted legacy feature vector",
			slog.String("socketID", socket.ID()),
			slog.Int("length", len(features)),
		)
	}

	log.Printf("[handleNewRecording] Running classifier for socket %s\n", socket.ID())

	var predictions []drone.Prediction
	var templatePredictions []drone.Prediction
	var windowSummaries []drone.WindowPrediction

	// Sliding windows are incompatible with PANNS embeddings (which are for entire files)
	// Only use sliding windows for legacy feature extraction
	useSliding := audioSample.Duration >= socketMinSlidingAnalysisDurationSec && len(features) != 2048
	if useSliding {
		windowPredictions, windows, err := c.classifier.PredictWithSlidingWindows(
			audioSample.Samples,
			audioSample.SampleRate,
			socketSlidingWindowDurationSeconds,
			socketSlidingWindowOverlapSeconds,
		)
		if err != nil {
			logger.WarnContext(ctx, "sliding window analysis failed, falling back to single-pass",
				slog.String("socketID", socket.ID()),
				slog.Any("error", err),
			)
		} else {
			if len(windowPredictions) > 0 {
				predictions = windowPredictions
			}
			windowSummaries = windows
			logger.InfoContext(ctx, "applied sliding window analysis",
				slog.String("socketID", socket.ID()),
				slog.Int("windowCount", len(windowSummaries)),
			)
		}
	}

	if len(predictions) == 0 {
		var err error
		predictions, err = c.classifier.Predict(features)
		if err != nil {
			err := xerrors.New(err)
			log.Printf("[handleNewRecording] Classifier error for socket %s: %v\n", socket.ID(), err)
			logger.ErrorContext(ctx, "failed to run classifier", slog.Any("error", err))
			socket.Emit("analysisError", map[string]string{"message": "classifier error"})
			return
		}
	}

	if c.templateMatcher != nil {
		templatePredictions = c.templateMatcher.Predict(features)
		if len(templatePredictions) > 0 {
			predictions = drone.MergePredictions(predictions, templatePredictions)
		}
	}

	latency := time.Since(started).Seconds() * 1000

	// Get base threshold from environment or use default
	baseThresholdStr := utils.GetEnv("DRONE_CONFIDENCE_THRESHOLD", "0.55")
	baseThreshold, err := strconv.ParseFloat(baseThresholdStr, 64)
	if err != nil {
		baseThreshold = 0.55 // Default
	}

	// Use adaptive threshold based on SNR
	adjustedThreshold := baseThreshold
	if audioSample.SNRDb != 0.0 {
		adjustedThreshold = drone.AdaptiveThreshold(baseThreshold, audioSample.SNRDb)
	}

	isDrone := drone.DetermineDroneLikelyWithSNR(predictions, baseThreshold, audioSample.SNRDb)
	log.Printf("[handleNewRecording] Classification complete for socket %s: isDrone=%v, predictions=%d\n",
		socket.ID(), isDrone, len(predictions))

	if len(predictions) > 0 {
		best := predictions[0]
		logger.InfoContext(ctx, "classification complete",
			slog.String("socketID", socket.ID()),
			slog.Float64("latency_ms", latency),
			slog.Bool("isDrone", isDrone),
			slog.String("label", best.Label),
			slog.String("type", best.Type),
			slog.String("category", best.Category),
			slog.Float64("confidence", best.Confidence),
			slog.Int("support", best.Support),
		)
	} else {
		logger.InfoContext(ctx, "classification complete",
			slog.String("socketID", socket.ID()),
			slog.Float64("latency_ms", latency),
			slog.Bool("isDrone", isDrone),
			slog.String("label", ""),
			slog.Float64("confidence", 0),
		)
	}
	summary := drone.ClassificationSummary{
		Predictions:       predictions,
		IsDrone:           isDrone,
		LatencyMs:         latency,
		FeatureVector:     features,
		SNRDb:             audioSample.SNRDb,
		AdjustedThreshold: adjustedThreshold,
		Windows:           windowSummaries,
		Latitude:          recData.Latitude,
		Longitude:         recData.Longitude,
		RecordingPath:     audioSample.Persisted,
		TemplatePreds:     templatePredictions,
	}

	if len(predictions) > 0 {
		summary.PrimaryType = predictions[0].Type
	}

	// Save detection if it has location and predictions
	if summary.Latitude != nil && summary.Longitude != nil && len(summary.Predictions) > 0 {
		predictionsJSON, err := json.Marshal(summary.Predictions)
		if err == nil {
			detection := &models.Detection{
				Timestamp:     time.Now(),
				Latitude:      summary.Latitude,
				Longitude:     summary.Longitude,
				IsDrone:       summary.IsDrone,
				PrimaryType:   summary.PrimaryType,
				Confidence:    summary.Predictions[0].Confidence,
				SNRDb:         summary.SNRDb,
				LatencyMs:     summary.LatencyMs,
				Predictions:   json.RawMessage(predictionsJSON),
				RecordingPath: summary.RecordingPath,
			}
			if len(summary.Predictions) > 0 {
				detection.PrimaryLabel = summary.Predictions[0].Label
				detection.PrimaryCategory = summary.Predictions[0].Category
				if summary.Predictions[0].Metadata != nil {
					if country, ok := summary.Predictions[0].Metadata["country_of_origin"]; ok {
						detection.CountryOfOrigin = country
					}
				}
			}
			if err := detections.SaveDetection(detection); err != nil {
				log.Printf("[Socket] Failed to save detection: %v\n", err)
			} else {
				log.Printf("[Socket] Detection saved successfully\n")
			}
		}
	}

	log.Printf("[handleNewRecording] Preparing to emit classification for socket %s\n", socket.ID())
	logger.InfoContext(ctx, "emitting classification result",
		slog.String("socketID", socket.ID()),
		slog.Int("predictionCount", len(predictions)),
		slog.Bool("isDrone", isDrone),
	)

	// Emit classification result
	socket.Emit("classification", summary)
	log.Printf("[handleNewRecording] Emitted classification for socket %s\n", socket.ID())
	logger.InfoContext(ctx, "successfully emitted classification result",
		slog.String("socketID", socket.ID()),
	)
}
