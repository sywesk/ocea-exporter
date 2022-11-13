package main

import (
	"github.com/sywesk/ocea-exporter/oceaapi"
	"github.com/sywesk/ocea-exporter/oceaauth"
	"go.uber.org/zap"
	"os"
	"strings"
	"time"
)

func main() {
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic("failed to init zap: " + err.Error())
	}

	zap.ReplaceGlobals(logger)

	if len(os.Args) != 2 {
		println("usage: ocea-exporter <config file>")
		os.Exit(1)
	}

	if err := LoadConfig(os.Args[1]); err != nil {
		zap.L().Fatal("failed to load configuration", zap.Error(err))
	}

	username := GetConfig().Username
	password := GetConfig().Password

	if username == "" || password == "" {
		zap.L().Fatal("username and password cannot be empty")
	}

	tokenProvider := oceaauth.NewTokenProvider(username, password)
	client := oceaapi.NewClient(tokenProvider)

	go setupMetricsHandler()

	refreshCounters(client)
}

func refreshCounters(client oceaapi.APIClient) {
	logger := zap.L()
	ticker := time.NewTicker(1 * time.Hour)

	firstRun := true
	for {
		if !firstRun {
			<-ticker.C
		} else {
			firstRun = false
		}

		counters, err := fetchCounters(client)
		if err != nil {
			logger.Error("failed to fetch counters", zap.Error(err))
			continue
		}

		logger.Info("fetched counters", zap.Any("counters", counters))

		for fluid, values := range counters {
			fluid = strings.ToLower(fluid)

			monthToDate.WithLabelValues(fluid).Set(values.MonthToDate)
			yearToDate.WithLabelValues(fluid).Set(values.YearToDate)
		}
	}
}
