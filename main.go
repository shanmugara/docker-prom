package main

import (
	"context"
	"log"
	"net/http"
	"time"

	//"github.com/docker/docker/api/types"
	types "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	containers, err := cli.ContainerList(ctx, types.ListOptions{})
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

func main() {
	// Create Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Error creating Docker client: %v", err)
	}

	// Start Prometheus HTTP server
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		log.Println("Starting Prometheus metrics server on :8000")
		log.Fatal(http.ListenAndServe(":8000", nil))
	}()

	// Continuously collect metrics at intervals
	for {
		collectDockerMetrics(cli)
		time.Sleep(10 * time.Second)
	}
}
