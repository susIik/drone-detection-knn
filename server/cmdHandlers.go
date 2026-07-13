package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"song-recognition/detections"
	"song-recognition/drone"
	"song-recognition/embedding"
	"song-recognition/models"
	"song-recognition/utils"

	socketio "github.com/googollee/go-socket.io"
	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/engineio/transport"
	"github.com/googollee/go-socket.io/engineio/transport/polling"
	"github.com/googollee/go-socket.io/engineio/transport/websocket"
	"github.com/mdobak/go-xerrors"
)

type apiError struct {
	Message string `json:"message"`
}

type prototypeUploadResponse struct {
	Added []drone.Prototype `json:"added"`
	Stats drone.ModelStats  `json:"stats"`
}

const (
	slidingWindowDurationSeconds  = 3.0
	slidingWindowOverlapSeconds   = 1.5
	minSlidingAnalysisDurationSec = 4.0
)

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("failed to encode JSON response: %v", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, apiError{Message: message})
}

func newPrototypeUploadHandler(classifier *drone.Classifier) http.HandlerFunc {
	logger := utils.GetLogger()
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if err := r.ParseMultipartForm(256 << 20); err != nil {
			logger.ErrorContext(ctx, "failed to parse multipart form", slog.Any("error", err))
			writeJSONError(w, http.StatusBadRequest, "invalid upload payload")
			return
		}

		label := strings.TrimSpace(r.FormValue("label"))
		if label == "" {
			writeJSONError(w, http.StatusBadRequest, "label is required")
			return
		}

		category := strings.TrimSpace(r.FormValue("category"))
		if category == "" {
			category = "drone"
		}

		description := strings.TrimSpace(r.FormValue("description"))

		if r.MultipartForm == nil {
			logger.ErrorContext(ctx, "multipart form is nil after parsing")
			writeJSONError(w, http.StatusBadRequest, "invalid upload payload")
			return
		}

		metadata := map[string]string{}
		if r.MultipartForm.Value != nil {
			for key, values := range r.MultipartForm.Value {
				if len(values) == 0 {
					continue
				}
				value := strings.TrimSpace(values[len(values)-1])
				switch key {
				case "label", "category", "description":
					continue
				case "model", "type", "rotor_count", "manufacturer", "drone_origin",
					"threat_level", "risk_category", "payload_capacity_kg",
					"max_range_km", "max_speed_ms", "flight_time_minutes",
					"jamming_susceptible", "countermeasure_recommendations",
					"max_altitude_m", "weight_kg", "has_gps", "has_camera",
					"has_autonomous_flight", "swarm_capable", "operator_type",
					"typical_use_cases", "detection_range_m":
					if value != "" {
						metadata[key] = value
					}
				default:
					if strings.HasPrefix(key, "meta[") && strings.HasSuffix(key, "]") {
						field := strings.TrimSuffix(strings.TrimPrefix(key, "meta["), "]")
						if field != "" && value != "" {
							metadata[field] = value
						}
					}
				}
			}
		}

		var files []*multipart.FileHeader
		if r.MultipartForm.File != nil {
			files = r.MultipartForm.File["samples"]
		}
		if len(files) == 0 {
			writeJSONError(w, http.StatusBadRequest, "no audio samples provided")
			return
		}

		tempDir := filepath.Join("tmp", "uploads")
		if err := utils.CreateFolder(tempDir); err != nil {
			logger.ErrorContext(ctx, "failed to create temporary upload dir", slog.Any("error", err))
			writeJSONError(w, http.StatusInternalServerError, "internal error while preparing upload")
			return
		}

		var added []drone.Prototype
		for _, fileHeader := range files {
			src, err := fileHeader.Open()
			if err != nil {
				logger.ErrorContext(ctx, "failed to open uploaded file", slog.Any("error", err))
				continue
			}
			defer src.Close()

			tempFile, err := os.CreateTemp(tempDir, "upload-*.wav")
			if err != nil {
				logger.ErrorContext(ctx, "failed to create temp file", slog.Any("error", err))
				src.Close()
				continue
			}

			_, err = io.Copy(tempFile, src)
			if err != nil {
				logger.ErrorContext(ctx, "failed to persist upload", slog.Any("error", err))
				tempFile.Close()
				os.Remove(tempFile.Name())
				src.Close()
				continue
			}
			tempFile.Close()
			src.Close()

			audioPath := tempFile.Name()
			prototype, err := drone.BuildPrototypeFromPath(audioPath, label, category, description, fileHeader.Filename, metadata)
			if err != nil {
				logger.ErrorContext(ctx, "failed to build prototype", slog.Any("error", err))
				os.Remove(audioPath)
				continue
			}

			stored, err := classifier.AddPrototype(prototype)
			if err != nil {
				logger.ErrorContext(ctx, "failed to register prototype", slog.Any("error", err))
				os.Remove(audioPath)
				continue
			}

			added = append(added, stored)
		}

		// Persist prototypes to disk if any were successfully added
		if len(added) > 0 {
			if err := classifier.SavePrototypesToFile(); err != nil {
				logger.ErrorContext(ctx, "failed to save prototypes to disk", slog.Any("error", err))
				// Continue anyway - prototypes are in memory, just not persisted
			} else {
				logger.InfoContext(ctx, "persisted prototypes to disk", slog.Int("count", len(added)))
			}
		}

		stats := classifier.Stats()
		writeJSON(w, http.StatusOK, prototypeUploadResponse{
			Added: added,
			Stats: stats,
		})
	}
}

func newAudioClassificationHandler(classifier *drone.Classifier, templateMatcher *drone.TemplateMatcher, persistRecordings bool) http.HandlerFunc {
	logger := utils.GetLogger()
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var recData models.RecordData
		if err := json.NewDecoder(r.Body).Decode(&recData); err != nil {
			logger.ErrorContext(ctx, "failed to parse request body", slog.Any("error", err))
			writeJSONError(w, http.StatusBadRequest, "invalid request payload")
			return
		}

		log.Printf("[HTTP] Audio classification request: sampleRate=%d, channels=%d, duration=%.2f, lat=%v, lng=%v\n",
			recData.SampleRate, recData.Channels, recData.Duration, recData.Latitude, recData.Longitude)

		if recData.Audio == "" {
			logger.ErrorContext(ctx, "no audio data received")
			writeJSONError(w, http.StatusBadRequest, "no audio data received")
			return
		}

		started := time.Now()

		audioSample, err := drone.PrepareAudioSample(recData, persistRecordings)
		if err != nil {
			err := xerrors.New(err)
			logger.ErrorContext(ctx, "failed to prepare audio sample", slog.Any("error", err))
			writeJSONError(w, http.StatusBadRequest, "unable to decode audio")
			return
		}

		logger.InfoContext(ctx, "prepared audio sample",
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
			embedding, err := pannsClient.EmbedFile(audioSample.Persisted)
			if err != nil {
				logger.WarnContext(ctx, "PANNS embedding failed, falling back to legacy features",
					slog.Any("error", err))
				// Fall back to old feature extraction
				features, err = drone.ExtractFeatureVector(audioSample.Samples, audioSample.SampleRate)
				if err != nil {
					err := xerrors.New(err)
					logger.ErrorContext(ctx, "failed to extract features", slog.Any("error", err))
					writeJSONError(w, http.StatusInternalServerError, "unable to extract features")
					return
				}
			} else {
				features = embedding
				logger.InfoContext(ctx, "extracted PANNS embedding",
					slog.Int("dimension", len(features)),
				)
			}
		} else {
			// Use legacy feature extraction
			features, err = drone.ExtractFeatureVector(audioSample.Samples, audioSample.SampleRate)
			if err != nil {
				err := xerrors.New(err)
				logger.ErrorContext(ctx, "failed to extract features", slog.Any("error", err))
				writeJSONError(w, http.StatusInternalServerError, "unable to extract features")
				return
			}
			logger.InfoContext(ctx, "extracted legacy feature vector",
				slog.Int("length", len(features)),
			)
		}

		var predictions []drone.Prediction
		var templatePredictions []drone.Prediction
		var windowSummaries []drone.WindowPrediction

		// Sliding windows are incompatible with PANNS embeddings (which are for entire files)
		// Only use sliding windows for legacy feature extraction
		useSliding := audioSample.Duration >= minSlidingAnalysisDurationSec && len(features) != 2048
		if useSliding {
			windowPredictions, windows, err := classifier.PredictWithSlidingWindows(
				audioSample.Samples,
				audioSample.SampleRate,
				slidingWindowDurationSeconds,
				slidingWindowOverlapSeconds,
			)
			if err != nil {
				logger.WarnContext(ctx, "sliding window analysis failed, falling back to single-pass",
					slog.Any("error", err),
				)
			} else {
				if len(windowPredictions) > 0 {
					predictions = windowPredictions
				}
				windowSummaries = windows
				logger.InfoContext(ctx, "applied sliding window analysis",
					slog.Int("windowCount", len(windowSummaries)),
				)
			}
		} else if len(features) == 2048 {
			logger.InfoContext(ctx, "using PANNS whole-file embedding (skipping sliding windows)")
		}

		if len(predictions) == 0 {
			predictions, err = classifier.Predict(features)
			if err != nil {
				err := xerrors.New(err)
				logger.ErrorContext(ctx, "failed to run classifier", slog.Any("error", err))
				writeJSONError(w, http.StatusInternalServerError, "classifier error")
				return
			}
		}

		if templateMatcher != nil {
			templatePredictions = templateMatcher.Predict(features)
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

		log.Printf("[HTTP] Classification complete: isDrone=%v, predictions=%d, latency=%.2fms\n",
			isDrone, len(predictions), latency)

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

		log.Printf("[HTTP] Returning classification with location: lat=%v, lng=%v\n", summary.Latitude, summary.Longitude)
		writeJSON(w, http.StatusOK, summary)
	}
}

func newDetectionsHandler() http.HandlerFunc {
	logger := utils.GetLogger()
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		detectionsList, err := detections.LoadDetections()
		if err != nil {
			logger.ErrorContext(ctx, "failed to load detections", slog.Any("error", err))
			writeJSONError(w, http.StatusInternalServerError, "failed to load detections")
			return
		}

		writeJSON(w, http.StatusOK, detectionsList)
	}
}

func serve(protocol, port string) {
	protocol = strings.ToLower(protocol)
	var allowOriginFunc = func(r *http.Request) bool {
		return true
	}

	modelPath := utils.GetEnv("DRONE_MODEL_PATH", filepath.Join("drone", "prototypes.json"))
	neighborCountStr := utils.GetEnv("DRONE_MODEL_K", "5")
	k, err := strconv.Atoi(neighborCountStr)
	if err != nil {
		log.Fatalf("invalid DRONE_MODEL_K value '%s': %v", neighborCountStr, err)
	}

	// Load classifier first to check prototype count
	classifier, err := drone.NewClassifierFromFile(modelPath, k)
	if err != nil {
		log.Fatalf("failed to load drone classifier: %v", err)
	}

	// Adaptive K based on prototype count (if few prototypes, use smaller K)
	stats := classifier.Stats()
	prototypeCount := stats.PrototypeCount

	// If we have fewer prototypes than K, adjust K
	if prototypeCount > 0 && k > prototypeCount {
		k = prototypeCount
		log.Printf("Adjusted K to %d (prototype count: %d)", k, prototypeCount)
		// Reload with adjusted K
		classifier, err = drone.NewClassifierFromFile(modelPath, k)
		if err != nil {
			log.Fatalf("failed to reload classifier with adjusted K: %v", err)
		}
	}
	// If we have very few prototypes, use smaller K for better reliability
	if prototypeCount < 10 && k > 3 {
		k = 3
		log.Printf("Using K=3 for small prototype set (%d prototypes)", prototypeCount)
		classifier, err = drone.NewClassifierFromFile(modelPath, k)
		if err != nil {
			log.Fatalf("failed to reload classifier with K=3: %v", err)
		}
	}

	templatePath := utils.GetEnv("DRONE_TEMPLATE_PATH", "")
	if templatePath == "" {
		defaultTemplatePath := filepath.Join("drone", "templates.json")
		if _, err := os.Stat(defaultTemplatePath); err == nil {
			templatePath = defaultTemplatePath
			log.Printf("DRONE_TEMPLATE_PATH not set, using default %s\n", templatePath)
		}
	}
	templateThresholdStr := utils.GetEnv("DRONE_TEMPLATE_THRESHOLD", "0.75")
	templateThreshold, err := strconv.ParseFloat(templateThresholdStr, 64)
	if err != nil {
		templateThreshold = 0.75
	}

	var templateMatcher *drone.TemplateMatcher
	if templatePath != "" {
		if matcher, tmErr := drone.NewTemplateMatcherFromFile(templatePath, templateThreshold); tmErr != nil {
			log.Printf("Failed to load template matcher (%s): %v\n", templatePath, tmErr)
		} else {
			log.Printf("Loaded %d templates from %s (threshold=%.2f)\n", matcher.TemplateCount(), templatePath, templateThreshold)
			templateMatcher = matcher
		}
	}

	persistRecordings := strings.EqualFold(utils.GetEnv("DRONE_PERSIST_RECORDINGS", "true"), "true")
	controller := newSocketController(classifier, templateMatcher, persistRecordings)

	server := socketio.NewServer(&engineio.Options{
		PingTimeout:  60 * time.Second,
		PingInterval: 25 * time.Second,
		Transports: []transport.Transport{
			&websocket.Transport{
				CheckOrigin: allowOriginFunc,
			},
			&polling.Transport{
				CheckOrigin: allowOriginFunc,
			},
		},
	})

	server.OnConnect("/", func(socket socketio.Conn) error {
		socket.SetContext("")
		connURL := socket.URL()
		log.Printf("CONNECTED: %s, transport: %s, remote addr: %s\n", socket.ID(), connURL.String(), socket.RemoteAddr())
		controller.emitModelInfo(socket)
		return nil
	})

	server.OnEvent("/", "requestModelInfo", func(socket socketio.Conn) {
		log.Printf("requestModelInfo received from %s\n", socket.ID())
		controller.handleRequestModelInfo(socket)
	})

	server.OnEvent("/", "newRecording", func(socket socketio.Conn, msg string) {
		log.Printf("=== newRecording event received from %s, data length: %d ===\n", socket.ID(), len(msg))
		if len(msg) > 100 {
			log.Printf("First 100 chars of payload: %s...\n", msg[:100])
		} else {
			log.Printf("Full payload: %s\n", msg)
		}
		// Run handler in goroutine to prevent blocking, with panic recovery
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("panic in handleNewRecording for socket %s: %v\n", socket.ID(), r)
					socket.Emit("analysisError", map[string]string{"message": "internal server error during processing"})
				}
			}()
			controller.handleNewRecording(socket, msg)
		}()
	})

	server.OnError("/", func(s socketio.Conn, e error) {
		log.Println("meet error:", e)
	})

	server.OnDisconnect("/", func(s socketio.Conn, reason string) {
		log.Printf("Socket disconnected - ID: %s, Reason: %s\n", s.ID(), reason)
	})

	go func() {
		if err := server.Serve(); err != nil {
			log.Fatalf("socketio listen error: %s\n", err)
		}
	}()
	defer server.Close()

	serveHTTPS := protocol == "https"

	uploadHandler := newPrototypeUploadHandler(classifier)
	classificationHandler := newAudioClassificationHandler(classifier, templateMatcher, persistRecordings)
	detectionsHandler := newDetectionsHandler()
	mux := http.NewServeMux()
	mux.Handle("/socket.io/", server)
	mux.HandleFunc("/api/prototypes/upload", uploadHandler)
	mux.HandleFunc("/api/audio/classify", classificationHandler)
	mux.HandleFunc("/api/detections", detectionsHandler)
	mux.Handle("/", http.FileServer(http.Dir("static")))

	serveHTTP(server, serveHTTPS, port, mux)
}

func serveHTTP(socketServer *socketio.Server, serveHTTPS bool, port string, handler http.Handler) {
	if handler == nil {
		handler = socketServer
	}
	if serveHTTPS {
		httpsAddr := ":" + port
		httpsServer := &http.Server{
			Addr: httpsAddr,
			TLSConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			Handler: handler,
		}

		cert_key_default := "/etc/letsencrypt/live/localport.online/privkey.pem"
		cert_file_default := "/etc/letsencrypt/live/localport.online/fullchain.pem"

		cert_key := utils.GetEnv("CERT_KEY", cert_key_default)
		cert_file := utils.GetEnv("CERT_FILE", cert_file_default)
		if cert_key == "" || cert_file == "" {
			log.Fatal("Missing cert")
		}

		log.Printf("Starting HTTPS server on %s\n", httpsAddr)
		if err := httpsServer.ListenAndServeTLS(cert_file, cert_key); err != nil {
			log.Fatalf("HTTPS server ListenAndServeTLS: %v", err)
		}
	}

	log.Printf("Starting HTTP server on port %v", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("HTTP server ListenAndServe: %v", err)
	}
}
