package main

import (
	"fmt"
	"os"

	"github.com/FelixSeptem/danny/internal/config"
)

func main() {
	cfg, err := config.LoadFromEnv(os.LookupEnv)
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("configuration valid: storage=%s listen=%s scheduler=%t\n", cfg.StorageDriver, cfg.ListenAddress, cfg.SchedulerEnabled)
}
