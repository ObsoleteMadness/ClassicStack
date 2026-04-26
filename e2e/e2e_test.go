package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMiniVMacE2E(t *testing.T) {
	// Check if minivmac is installed
	if _, err := exec.LookPath("minivmac"); err != nil {
		t.Skip("minivmac not found in PATH, skipping e2e test")
	}

	// Check if python3 is installed
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found in PATH, skipping e2e test")
	}

	tempDir := t.TempDir()
	dskPath := filepath.Join(tempDir, "test.dsk")

	// 1. Build the test disk image using python script
	buildCmd := exec.Command("python3", "build_image.py", "test.applescript", dskPath)
	buildCmd.Dir = "." // Ensure we are running where scripts are
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build disk image: %v", err)
	}

	// 2. Start the omnitalk test server.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCmd := exec.CommandContext(ctx, "go", "run", "../cmd/omnitalk")
	serverCmd.Stdout = os.Stdout
	serverCmd.Stderr = os.Stderr
	if err := serverCmd.Start(); err != nil {
		t.Fatalf("Failed to start omnitalk server: %v", err)
	}

	// Give the server a few seconds to start up
	time.Sleep(2 * time.Second)

	// 3. Launch minivmac
	// Note: in a real environment, this also requires a bootable System disk to be passed.
	// For this test scaffold, we pass the test disk image.
	minivmacCmd := exec.Command("minivmac", dskPath)
	minivmacCmd.Env = append(os.Environ(), "SDL_VIDEODRIVER=dummy", "SDL_AUDIODRIVER=dummy")

	if err := minivmacCmd.Start(); err != nil {
		t.Fatalf("Failed to start minivmac: %v", err)
	}

	// 4. Wait up to 60 seconds for the test to complete
	done := make(chan error, 1)
	go func() {
		time.Sleep(60 * time.Second)
		minivmacCmd.Process.Kill()
		done <- nil
	}()

	<-done

	// 5. Extract results using python script
	extractCmd := exec.Command("python3", "extract_results.py", dskPath)
	extractCmd.Dir = "."
	output, err := extractCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to extract results: %v\nOutput: %s", err, string(output))
	}

	results := string(output)
	t.Logf("Results from AppleScript:\n%s", results)

	if !strings.Contains(results, "Test Started") {
		t.Errorf("Expected results to contain 'Test Started', got:\n%s", results)
	}

	if !strings.Contains(results, "Test Completed") {
		t.Errorf("Expected results to contain 'Test Completed', got:\n%s", results)
	}
}
