package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kp-cms/server/internal/config"
	"github.com/kp-cms/server/internal/server"
)

var (
	version = "dev"
)

func main() {
	dataDir := flag.String("data-dir", "", "Data directory for storage")
	port := flag.Int("port", 0, "HTTP server port (0 = use config/default)")
	host := flag.String("host", "", "HTTP server host")
	mode := flag.String("mode", "", "Run mode: standalone, subprocess")
	appname := flag.String("appname", "", "Application name")
	printVersion := flag.Bool("version", false, "Print version and exit")

	flag.Parse()

	if *printVersion {
		fmt.Printf("kp-cms server version %s\n", version)
		os.Exit(0)
	}

	cfg, err := config.Load(*dataDir, *appname)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	if *port != 0 {
		cfg.Port = *port
	}
	if *host != "" {
		cfg.Host = *host
	}
	if *mode != "" {
		cfg.Mode = config.Mode(*mode)
	}
	if *appname != "" {
		cfg.AppName = *appname
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	if err := server.Run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
