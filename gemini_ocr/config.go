// YAML configuration for gemini_ocr, loaded via viper from a --config file
// (default config.yaml, per AGENT.md's Golang conventions). Any field left
// unset (zero value) in the file falls back to its equivalent env var, then
// to a hardcoded default — existing env-var-only deployments (e.g.
// scripts/run_pipeline.sh) keep working unchanged. POST /config/refresh
// (see main.go) re-reads the file and re-applies it without a restart.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/spf13/viper"
	"google.golang.org/genai"
)

type Config struct {
	GeminiAPIKey   string `mapstructure:"gemini_api_key"`
	GeminiModel    string `mapstructure:"gemini_model"`
	MaxConcurrency int    `mapstructure:"max_concurrency"`
	WorkspaceRoot  string `mapstructure:"workspace_root"`
	SaveResultsDir string `mapstructure:"save_results_dir"`
}

// configPath is the --config value, kept so /config/refresh re-reads the
// same file main() loaded at startup.
var configPath string

// configMu guards the resolved settings below, since /config/refresh can
// rewrite them while /tasks and /health requests are reading them.
var configMu sync.RWMutex

var (
	workspaceRoot         string
	defaultModel          string
	defaultMaxConcurrency int
	saveResultsDir        string
	geminiClient          *genai.Client
)

// loadConfig reads and parses path with viper. A missing file is not an
// error — it just means every field falls back to its env var/default.
func loadConfig(path string) (Config, error) {
	var cfg Config
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// applyConfig resolves cfg (falling back to env vars, then hardcoded
// defaults) and swaps it into the package-level settings under configMu.
// Used both at startup and by POST /config/refresh.
func applyConfig(cfg Config) error {
	apiKey := cfg.GeminiAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("gemini_api_key must be set (config.yaml's gemini_api_key, or the GEMINI_API_KEY/GOOGLE_API_KEY env var)")
	}

	model := cfg.GeminiModel
	if model == "" {
		model = os.Getenv("GEMINI_MODEL")
	}
	if model == "" {
		model = "gemini-2.5-flash-lite"
	}

	maxConcurrency := cfg.MaxConcurrency
	if maxConcurrency == 0 {
		if v := os.Getenv("MAX_CONCURRENCY"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("invalid MAX_CONCURRENCY: %w", err)
			}
			maxConcurrency = n
		} else {
			maxConcurrency = 4
		}
	}

	wsRoot := cfg.WorkspaceRoot
	if wsRoot == "" {
		wsRoot = os.Getenv("WORKSPACE_ROOT")
	}
	if wsRoot == "" {
		return fmt.Errorf("workspace_root must be set (config.yaml's workspace_root, or the WORKSPACE_ROOT env var)")
	}

	resultsDir := cfg.SaveResultsDir
	if resultsDir == "" {
		resultsDir = os.Getenv("SAVE_RESULTS_DIR")
	}
	if resultsDir == "" {
		resultsDir = "gemini_ocr/output.json"
	}

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return fmt.Errorf("create gemini client: %w", err)
	}

	configMu.Lock()
	defer configMu.Unlock()
	defaultModel = model
	defaultMaxConcurrency = maxConcurrency
	workspaceRoot = wsRoot
	saveResultsDir = resultsDir
	geminiClient = client
	return nil
}
