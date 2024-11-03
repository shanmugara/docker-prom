package main

import (
	"context"
	"flag"
	"fmt"
	typeContainer "github.com/docker/docker/api/types/container"
	"log"
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
	// Register the Prometheus metric
	prometheus.MustRegister(containerImageInfo)
}

func collectDockerMetrics(cli *client.Client) {
	ctx := context.Background()

	// List all containers
	containers, err := cli.ContainerList(ctx, typeContainer.ListOptions{})
	if err != nil {
		log.Printf("Error listing containers: %v", err)
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
			log.Printf("Error inspecting image for container %s: %v", containerName, err)
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
		return fmt.Errorf("error registering metric: %w", err)
	}

	promFile := filepath.Join(metricsFilePath, "docker_metrics.prom")
	file, err := os.OpenFile(promFile, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)

	if err != nil {
		return fmt.Errorf("error opening metrics file: %w", err)
	}
	defer file.Close()

	// Gather metrics and encode in Prometheus text format
	gatherers := prometheus.Gatherers{registry}
	metrics, err := gatherers.Gather()
	if err != nil {
		return fmt.Errorf("error gathering metrics: %w", err)
	}

	encoder := expfmt.NewEncoder(file, PromText)
	for _, metric := range metrics {
		if err := encoder.Encode(metric); err != nil {
			return fmt.Errorf("error encoding metrics: %w", err)
		}
	}
	return nil
}

func main() {
	port := flag.String("port", "8000", "Port to listen on for Prometheus metrics")
	metricsFilePath := flag.String("metricsFilePath", "", "Path to write Prometheus metrics (disables HTTP listener if set)")
	interval := flag.Duration("interval", 10, "Interval to collect metrics")
	flag.Parse()

	// Create Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Error creating Docker client: %v", err)
	}

	// Disable HTTP listener if metricsFile is specified
	if *metricsFilePath == "" {
		// Start Prometheus HTTP server
		http.Handle("/metrics", promhttp.Handler())
		go func() {
			log.Printf("Starting Prometheus metrics server on :%s", *port)
			log.Fatal(http.ListenAndServe(":"+*port, nil))
		}()
	}

	// Continuously collect metrics and either write to file or expose over HTTP
	for {
		collectDockerMetrics(cli)

		if *metricsFilePath != "" {
			if err := writeMetricsToFile(*metricsFilePath, containerImageInfo); err != nil {
				log.Printf("Error writing metrics to file: %v", err)
			}
		}

		time.Sleep(*interval * time.Second)
	}
}
