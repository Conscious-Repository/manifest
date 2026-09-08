package labmodel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConduitConfiguration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LAB_MODEL_URL", "")
	t.Setenv("LAB_MODEL_NAME", "")
	if BaseURL() != "" || Model() != "deepseek-v4-flash-vision-exp" {
		t.Fatal("defaults")
	}
	if err := os.MkdirAll(filepath.Dir(URLPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(URLPath(), []byte(" http://lab.test/v1/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if BaseURL() != "http://lab.test/v1" {
		t.Fatal(BaseURL())
	}
	t.Setenv("LAB_MODEL_URL", " http://override.test/v1/ ")
	t.Setenv("LAB_MODEL_NAME", "custom-model")
	if BaseURL() != "http://override.test/v1" || Model() != "custom-model" {
		t.Fatal("override")
	}
}
