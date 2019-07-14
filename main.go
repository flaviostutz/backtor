package main

import (
	"flag"
	"os"

	"github.com/flaviostutz/backtor/backtor"
	"github.com/sirupsen/logrus"
)

var options backtor.Options

func main() {
	conductorAPIURL := flag.String("conductor-api-url", "", "Base Conductor API URL for calling backup workflows")
	logLevel := flag.String("log-level", "info", "debug, info, warning or error")
	dataDir := flag.String("data-dir", "/var/lib/backtor/data", "debug, info, warning or error")
	flag.Parse()

	switch *logLevel {
	case "debug":
		logrus.SetLevel(logrus.DebugLevel)
		break
	case "warning":
		logrus.SetLevel(logrus.WarnLevel)
		break
	case "error":
		logrus.SetLevel(logrus.ErrorLevel)
		break
	default:
		logrus.SetLevel(logrus.InfoLevel)
	}

	logrus.Debug("Preparing options")
	options.ConductorAPIURL = *conductorAPIURL
	options.DataDir = *dataDir

	if options.ConductorAPIURL == "" {
		logrus.Error("--conductor-api-url is required")
		os.Exit(1)
	}

	if options.DataDir == "" {
		logrus.Error("--data-dir cannot be empty")
		os.Exit(1)
	}

	logrus.Infof("====Starting backtor====")

	err := backtor.InitAll(options)
	if err != nil {
		logrus.Errorf("Failed to initialize backtor. err=%s", err)
		os.Exit(1)
	}
}
