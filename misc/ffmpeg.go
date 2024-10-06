package misc

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/1rvyn/halloween-story-generator/models"
)

type SegmentVideo struct {
	Segment   models.Segment
	VideoPath string
	AudioPath string
}

// GenerateFfmpegInputFile handles the video creation process using ffmpeg and OpenAI TTS.
func GenerateFfmpegInputFile(storyID int, segments []models.Segment) (string, error) {
	startTime := time.Now()
	story := struct {
		ID       int
		Segments []SegmentVideo
	}{
		ID:       storyID,
		Segments: make([]SegmentVideo, len(segments)),
	}

	for i, seg := range segments {
		story.Segments[i] = SegmentVideo{
			Segment: seg,
		}
	}

	// Frame rate
	frameRate := 6

	tempDir := ""
	if runtime.GOOS == "darwin" {
		tempDir = "/tmp/temp" // Use macOS temporary directory
	} else {
		tempDir = "/dev/shm/temp" // Use Linux RAM-backed storage
	}
	// Use RAM-backed storage for faster disk operations
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		err := os.Mkdir(tempDir, 0755)
		if err != nil {
			log.Fatalf("Failed to create temp directory: %v", err)
		}
	}

	// Pre-allocate slice to hold paths of temporary segment videos
	segmentVideos := make([]string, len(story.Segments))
	sem := make(chan struct{}, 2) // Limit to 2 concurrent FFmpeg processes

	// WaitGroup to synchronize goroutines
	var wg sync.WaitGroup

	// Channel to capture errors from goroutines
	errChan := make(chan error, len(story.Segments))

	for idx, segment := range story.Segments {
		idx := idx         // capture loop variable
		segment := segment // capture loop variable
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			// Generate TTS audio for the segment text and get duration
			audioPath, audioDuration, err := getTTS(segment.Segment.Segment, story.ID, idx, tempDir)
			if err != nil {
				errChan <- err
				return
			}

			// Temporary video path for the segment
			segmentVideoPath := filepath.Join(tempDir, fmt.Sprintf("segment_%d.mp4", idx+1))
			segmentVideos[idx] = segmentVideoPath

			// Calculate the number of frames for the segment
			segmentFrames := int(audioDuration * float64(frameRate))

			// Construct the filter complex
			filterComplex := fmt.Sprintf(
				"[0]scale=1344:-2,setsar=1:1[out];[out]crop=1344:768[out];[out]scale=4000:-1,zoompan=z='zoom+0.001':x=iw/2-(iw/zoom/2):y=ih/2-(ih/zoom/2):d=%d:s=1344x768:fps=%d[out]",
				segmentFrames, frameRate,
			)

			// FFmpeg command to create a video for the segment with audio merged in one step
			ffmpegCmd := exec.Command("ffmpeg",
				"-y",
				"-f", "image2pipe",
				"-i", "pipe:0",
				"-i", audioPath,
				"-filter_complex", filterComplex,
				"-c:v", "libx264",
				"-preset", "ultrafast",
				"-tune", "stillimage",
				"-t", fmt.Sprintf("%.2f", audioDuration),
				"-pix_fmt", "yuv420p",
				"-r", fmt.Sprintf("%d", frameRate),
				"-threads", "4",
				"-map", "[out]",
				"-map", "1:a",
				"-shortest",
				segmentVideoPath,
			)

			// Create a pipe to write the image data
			stdin, err := ffmpegCmd.StdinPipe()
			if err != nil {
				errChan <- fmt.Errorf("error creating stdin pipe: %w", err)
				return
			}

			// Capture stderr for debugging
			var stderr bytes.Buffer
			ffmpegCmd.Stderr = &stderr

			// Start the FFmpeg command
			if err := ffmpegCmd.Start(); err != nil {
				errChan <- fmt.Errorf("error starting FFmpeg: %w", err)
				return
			}

			// Write the image data to stdin
			_, err = stdin.Write(segment.Segment.ImageData)
			if err != nil {
				errChan <- fmt.Errorf("error writing image data to stdin: %w", err)
				return
			}
			stdin.Close()

			log.Printf("Creating segment video with audio: %s", segmentVideoPath)
			if err := ffmpegCmd.Wait(); err != nil {
				log.Printf("FFmpeg error for segment %d: %v, Details: %s", idx+1, err, stderr.String())
				errChan <- err
				return
			}
		}()
	}

	// Wait for all segment creation goroutines to finish
	wg.Wait()
	close(errChan)

	// Check if any errors occurred during segment creation
	for err := range errChan {
		if err != nil {
			return "", err
		}
	}

	// Create the FFmpeg concat input file
	concatListPath := filepath.Join(tempDir, fmt.Sprintf("story_%d_concat.txt", story.ID))
	concatFile, err := os.Create(concatListPath)
	if err != nil {
		log.Printf("Error creating concat file: %v", err)
		return "", err
	}
	defer concatFile.Close()

	for _, segmentVideo := range segmentVideos {
		concatFile.WriteString(fmt.Sprintf("file '%s'\n", segmentVideo))
	}

	// Create the final video by concatenating all segment videos
	videoPath := filepath.Join(tempDir, fmt.Sprintf("story_%d_video.mp4", story.ID))
	ffmpegConcatCmd := exec.Command("ffmpeg",
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", concatListPath,
		"-c", "copy",
		"-fps_mode", "cfr",
		videoPath,
	)

	// Capture stderr for debugging
	var stderrConcat bytes.Buffer
	ffmpegConcatCmd.Stderr = &stderrConcat

	if err := ffmpegConcatCmd.Run(); err != nil {
		log.Printf("FFmpeg concat error: %v, Details: %s", err, stderrConcat.String())
		return "", err
	}

	duration := time.Since(startTime).Seconds()
	log.Printf("Total time taken: %.2f seconds", duration)

	// Clean up temporary segment videos, audio files, and concat file after final video creation
	defer func() {
		for i, segmentVideo := range segmentVideos {
			// Remove segment video
			if err := os.Remove(segmentVideo); err != nil {
				log.Printf("Warning: Failed to remove temporary video file %s: %v", segmentVideo, err)
			}

			// Remove corresponding audio file
			audioFile := filepath.Join(tempDir, fmt.Sprintf("speech_%d_seg_%d.mp3", story.ID, i))
			if err := os.Remove(audioFile); err != nil {
				log.Printf("Warning: Failed to remove temporary audio file %s: %v", audioFile, err)
			}
		}

		// Remove concat list file
		if err := os.Remove(concatListPath); err != nil {
			log.Printf("Warning: Failed to remove concat list file %s: %v", concatListPath, err)
		}
	}()

	return videoPath, nil
}

func getTTS(text string, storynumb, idx int, tempDir string) (string, float64, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", 0, fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}

	url := "https://api.openai.com/v1/audio/speech"

	// Escape special characters in the text
	escapedText := strings.ReplaceAll(text, "\"", "\\\"")
	escapedText = strings.ReplaceAll(escapedText, "\n", "\\n")

	payload := fmt.Sprintf(`{
		"model": "tts-1",
		"input": "%s",
		"voice": "onyx"
	}`, escapedText)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(payload)))
	if err != nil {
		return "", 0, err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	outputFile := filepath.Join(tempDir, fmt.Sprintf("speech_%d_seg_%d.mp3", storynumb, idx))
	out, err := os.Create(outputFile)
	if err != nil {
		return "", 0, err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", 0, err
	}

	// After saving the audio file
	// Get the duration of the audio file using ffprobe
	ffprobeCmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", outputFile)
	var durationStr bytes.Buffer
	ffprobeCmd.Stdout = &durationStr
	if err := ffprobeCmd.Run(); err != nil {
		return "", 0, err
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(durationStr.String()), 64)
	if err != nil {
		return "", 0, err
	}

	return outputFile, duration, nil
}
