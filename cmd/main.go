package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"nodeto/restic-csi-plugin/config"
    "nodeto/restic-csi-plugin/internal/server"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	var (
		nodeId         = flag.String("node-id", "", "The Node ID")
		endpoint       = flag.String("endpoint", "unix:///csi/csi.sock", "CSI endpoint")
		version        = flag.Bool("version", false, "Print the version and exit.")
		configFilePath = flag.String("config", "/local/config.toml", "Path to the configuration file")
		secretFilePath = flag.String("secret", "/secrets/secret.toml", "Path to the secret file")
	)
	flag.Parse()

	if *version {
		fmt.Printf("%s - %s (%s)\n", server.GetVersion(), server.GetCommit(), server.GetTreeState())
		os.Exit(0)
	}

	if len(*nodeId) < 1 {
		fmt.Println("node-id is required")
		os.Exit(1)
	}

	config, err := config.LoadConfig(*configFilePath, *secretFilePath)
	if err != nil {
		// Handle the error, for example, log it and exit
		log.Fatalf("Error loading configuration: %s", err)
	}

	// Log staging information
	log.Printf("Staging path: %s\n", config.VolumeInformation.StagingPath)
	log.Printf("Thin pool path: %s\n", config.VolumeInformation.ThinPoolName)


	// Print repositories
	for i, destination := range config.ResticRepo {
		log.Printf("Info: Destination %d of %d - %s", i+1, len(config.ResticRepo), destination.Repository)
	}

	log.Printf("Info: Using endpoint - %s", *endpoint)

	drv, err := server.NewDriver(*endpoint, "", *nodeId, &config)
	if err != nil {
		log.Fatalln(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
	}()

	if err := drv.Run(ctx); err != nil {
		log.Fatalln(err)
	}
}
