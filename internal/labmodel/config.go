// Package labmodel shares the lab conduit configuration with the portal and recruiting.
package labmodel

import (
	"os"
	"path/filepath"
	"strings"
)

const DefaultModel = "deepseek-v4-flash-vision-exp"

func URLPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "excalibur", "lab_model_url")
}

func BaseURL() string {
	if v := strings.TrimSpace(os.Getenv("LAB_MODEL_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	b, _ := os.ReadFile(URLPath())
	return strings.TrimRight(strings.TrimSpace(string(b)), "/")
}

func Model() string {
	if v := strings.TrimSpace(os.Getenv("LAB_MODEL_NAME")); v != "" {
		return v
	}
	return DefaultModel
}
