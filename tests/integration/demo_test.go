package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDynamicPluginPackage(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "freebsd" {
		t.Skip("Go plugins are not supported on this platform")
	}
	root := filepath.Clean(filepath.Join("..", ".."))
	output := filepath.Join(t.TempDir(), "freshguard.so")
	command := exec.Command("go", "build", "-buildmode=plugin", "-trimpath", "-o", output, "./cmd/plugin")
	command.Dir = root
	command.Env = os.Environ()
	if combined, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build plugin: %v\n%s", err, combined)
	}

	verify := exec.Command("go", "run", "-trimpath", "./tests/integration/cmd/verifyplugin", output)
	verify.Dir = root
	verify.Env = os.Environ()
	if combined, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("verify plugin exports: %v\n%s", err, combined)
	}
}

func TestExampleConfigPinsBifrostAndFreshGuard(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Plugins []struct {
			Name      string         `json:"name"`
			Path      string         `json:"path"`
			Placement string         `json:"placement"`
			Config    map[string]any `json:"config"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Plugins) != 1 || config.Plugins[0].Name != "freshguard" || config.Plugins[0].Path != "/plugins/freshguard.so" || config.Plugins[0].Placement != "post_builtin" {
		t.Fatalf("plugin config = %#v", config.Plugins)
	}
	if config.Plugins[0].Config["policy"] != "warn" {
		t.Fatalf("FreshGuard config = %#v", config.Plugins[0].Config)
	}
}
