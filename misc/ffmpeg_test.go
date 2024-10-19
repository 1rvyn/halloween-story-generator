package misc

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/1rvyn/halloween-story-generator/models"
)

// Mock environment variable for testing
func TestGetTTSSuccess(t *testing.T) {
	// Set a mock API key
	os.Setenv("OPENAI_API_KEY", "test")
	defer os.Unsetenv("OPENAI_API_KEY")

	text := "This is a test."
	storyNum := 1
	idx := 1
	tempDir := "/tmp/test_temp"

	// Create temp directory
	err := os.MkdirAll(tempDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	audioPath, duration, err := getTTS(text, storyNum, idx, tempDir)
	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}

	if audioPath == "" {
		t.Error("Expected audioPath to be set, got empty string")
	}

	if duration <= 0 {
		t.Errorf("Expected positive duration, got %f", duration)
	}
}

func TestGetTTSEmptyText(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "test")
	defer os.Unsetenv("OPENAI_API_KEY")

	text := ""
	storyNum := 1
	idx := 1
	tempDir := "/tmp/test_temp_empty"

	_, _, err := getTTS(text, storyNum, idx, tempDir)
	if err == nil {
		t.Error("Expected error for empty text, got nil")
	}
}

func TestGetTTSAPIFailure(t *testing.T) {
	// This test assumes that the API key is invalid or the API endpoint is unreachable
	os.Setenv("OPENAI_API_KEY", "invalid_api_key")
	defer os.Unsetenv("OPENAI_API_KEY")

	text := "This should fail."
	storyNum := 1
	idx := 2
	tempDir := "/tmp/test_temp_api_failure"

	// Create temp directory
	err := os.MkdirAll(tempDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	_, _, err = getTTS(text, storyNum, idx, tempDir)
	if err == nil {
		t.Error("Expected API failure, got nil")
	}
}

// this test checks the entire generateFfmpegInputFile function to make sure the ffmpeg matches the input
func TestGenerateFfmpegInputFileSuccess(t *testing.T) {
	// Setup
	os.Setenv("OPENAI_API_KEY", "test_api_key")
	defer os.Unsetenv("OPENAI_API_KEY")

	tempDir := "/tmp/temp"
	err := os.MkdirAll(tempDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Mock segments
	segments := []models.Segment{
		{
			Segment: "First segment text; there was once a large armadillo.",
			ImageData: func() []byte {
				data, err := os.ReadFile("test.png")
				if err != nil {
					panic(fmt.Sprintf("Failed to read image file: %v", err))
				}
				return data
			}(),
		},
		{
			Segment: "Second segment text, there was once a large crocodile.",
			ImageData: func() []byte {
				data, err := os.ReadFile("test.png")
				if err != nil {
					panic(fmt.Sprintf("Failed to read image file: %v", err))
				}
				return data
			}(),
		},
	}

	storyID := 1

	videoPath, err := GenerateFfmpegInputFile(storyID, segments)
	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}

	if videoPath == "" {
		t.Error("Expected videoPath to be set, got empty string")
	}

	// Optionally, check if the video file exists
	if _, err := os.Stat(videoPath); os.IsNotExist(err) {
		t.Errorf("Expected video file to exist at %s, but it does not", videoPath)
	}
}

func TestGenerateFfmpegInputFileTTSError(t *testing.T) {
	// Setup
	os.Setenv("OPENAI_API_KEY", "invalid_api_key")
	defer os.Unsetenv("OPENAI_API_KEY")

	tempDir := "/tmp/test_generate_ffmpeg_tts_error"
	err := os.MkdirAll(tempDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Mock segments with one segment expected to fail TTS
	segments := []models.Segment{
		{
			Segment:   "Valid segment text.",
			ImageData: []byte{}, // Assume valid image data
		},
		{
			Segment:   "",       // This should cause getTTS to fail
			ImageData: []byte{}, // Assume valid image data
		},
	}

	storyID := 2

	// Increase timeout for this test
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	done := make(chan struct{})
	var testErr error

	go func() {
		_, testErr = GenerateFfmpegInputFile(storyID, segments)
		close(done)
	}()

	select {
	case <-ctx.Done():
		t.Fatal("Test timed out")
	case <-done:
		if testErr == nil {
			t.Error("Expected error due to TTS failure, got nil")
		}
	}
}

func TestGenerateFfmpegInputFileNoSegments(t *testing.T) {
	// Setup
	os.Setenv("OPENAI_API_KEY", "test_api_key")
	defer os.Unsetenv("OPENAI_API_KEY")

	segments := []models.Segment{}

	storyID := 3

	_, err := GenerateFfmpegInputFile(storyID, segments)
	if err == nil {
		t.Error("Expected error due to no segments, got nil")
	}
}
