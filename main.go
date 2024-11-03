package main

import (
	"context"
	"flag"
	"fmt"
	typeContainer "github.com/docker/docker/api/types/container"
	"go.uber.org/zap"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/expfmt"
)

const (
	PromText expfmt.Format = "text/plain"
)

var (
	// logger
	logger *zap.Logger

	// Define Prometheus metric
	containerImageInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "docker_container_image_info",
			Help: "Docker container image information",
		},
		[]string{"container_name", "image_id", "image_repo"},
	)
)

func init() {
	// Init logger
	initLogger()
	// Register the Prometheus metric
	prometheus.MustRegister(containerImageInfo)
}

func initLogger() {
	// create a new zap logger
	var err error
	config := zap.NewProductionConfig()
	config.Level.SetLevel(zap.InfoLevel)
	if os.Getenv("DEBUG") == "true" {
		config.Level.SetLevel(zap.DebugLevel)
	} else {
		fmt.Println("DEBUG is not set")
		fmt.Println("in init DEBUG env vars is ", os.Getenv("DEBUG"))
	}

	logger, err = config.Build()
	if err != nil {
		fmt.Printf("Error creating zap logger: %v", err)
		os.Exit(1)
	}
}

func collectDockerMetrics(cli *client.Client) {
	ctx := context.Background()

	// List all containers
	containers, err := cli.ContainerList(ctx, typeContainer.ListOptions{})
	if err != nil {
		logger.Error("Error listing containers", zap.Error(err))
		return
	}

	// Clear old metrics to avoid duplicates
	containerImageInfo.Reset()

	// Collect metrics for each container
	for _, container := range containers {
		containerName := container.Names[0]
		imageID := container.ImageID

		// Fetch full image information
		image, _, err := cli.ImageInspectWithRaw(ctx, container.Image)
		if err != nil {
			logger.Error("Error inspecting image for container", zap.String("containerName", containerName), zap.Error(err))
			continue
		}

		// Get image repo tag; use "unknown" if no tags are available
		imageRepo := "unknown"
		if len(image.RepoTags) > 0 {
			imageRepo = image.RepoTags[0]
		}

		// Set the metric with container name, image ID, and repo path as labels
		containerImageInfo.WithLabelValues(containerName, imageID, imageRepo).Set(1)
	}
}

func writeMetricsToFile(metricsFilePath string, metric prometheus.Collector) error {
	// Create or truncate the file

	registry := prometheus.NewRegistry()
	if err := registry.Register(metric); err != nil {
		logger.Error("Error registering metric", zap.Error(err))
		return fmt.Errorf("error registering metric: %w", err)
	}

	promFile := filepath.Join(metricsFilePath, "docker_metrics.prom")
	logger.Debug("Writing metrics to file", zap.String("file", promFile))
	file, err := os.OpenFile(promFile, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)

	if err != nil {
		logger.Error("Error opening metrics file", zap.Error(err))
		return fmt.Errorf("error opening metrics file: %w", err)
	}
	defer file.Close()

	// Gather metrics and encode in Prometheus text format
	gatherers := prometheus.Gatherers{registry}
	metrics, err := gatherers.Gather()
	if err != nil {
		logger.Error("Error gathering metrics", zap.Error(err))
		return fmt.Errorf("error gathering metrics: %w", err)
	}

	encoder := expfmt.NewEncoder(file, PromText)
	for _, metric := range metrics {
		if err := encoder.Encode(metric); err != nil {
			logger.Error("Error encoding metrics", zap.Error(err))
			return fmt.Errorf("error encoding metrics: %w", err)
		}
	}
	logger.Debug("Metrics written to file")

	return nil
}

func main() {
	port := flag.String("port", "8000", "Port to listen on for Prometheus metrics")
	metricsFilePath := flag.String("metricsFilePath", "", "Path to write Prometheus metrics (disables HTTP listener if set)")
	interval := flag.Duration("interval", 10*time.Second, "Interval to collect metrics")
	debug := flag.Bool("debug", false, "Enable debug logging")

	flag.Parse()

	if err := os.Setenv("DEBUG", fmt.Sprintf("%t", *debug)); err != nil {
		fmt.Errorf("error setting DEBUG env variable: %w", err)
		os.Exit(1)
	}
	fmt.Println("in main DEBUG is set to ", os.Getenv("DEBUG"))

	defer logger.Sync()

	// Create Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Fatal("Error creating Docker client", zap.Error(err))
	}
	logger.Debug("Docker client created")

	// Disable HTTP listener if metricsFile is specified
	if *metricsFilePath == "" {
		// Start Prometheus HTTP server
		http.Handle("/metrics", promhttp.Handler())
		go func() {
			logger.Info("Starting Prometheus metrics server", zap.String("port", *port))
			if err := http.ListenAndServe(":"+*port, nil); err != nil {
				logger.Fatal("Error starting HTTP server", zap.Error(err))
			}
		}()
	} else {
		logger.Info("Metrics file path specified", zap.String("path", *metricsFilePath))
	}

	// Continuously collect metrics and either write to file or expose over HTTP
	for {
		collectDockerMetrics(cli)

		if *metricsFilePath != "" {
			if err := writeMetricsToFile(*metricsFilePath, containerImageInfo); err != nil {
				logger.Error("Error writing metrics to file", zap.Error(err))
			}
		}
		logger.Debug("Metrics collected, sleeping", zap.Duration("interval", *interval))
		time.Sleep(*interval)
	}
}
