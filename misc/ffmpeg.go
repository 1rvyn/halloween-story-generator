package misc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/1rvyn/halloween-story-generator/database"
	"github.com/1rvyn/halloween-story-generator/middleware"
	"github.com/1rvyn/halloween-story-generator/models"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	// Added AWS session import
)

type SegmentVideo struct {
	Segment   models.Segment
	VideoPath string
	AudioPath string
}

// GenerateFfmpegInputFile handles the video creation process using ffmpeg and OpenAI TTS.
func GenerateFfmpegInputFile(storyID int, segments []models.Segment) (string, error) {
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
	frameRate := 15

	// Ensure the temp directory exists
	tempDir := filepath.Join(".", "temp")
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		err := os.MkdirAll(tempDir, 0755)
		if err != nil {
			return "", fmt.Errorf("failed to create temp directory: %w", err)
		}
	}

	// Pre-allocate slice to hold paths of temporary segment videos
	segmentVideos := make([]string, len(story.Segments))

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

			// Download image from ImageURL
			imageURL := segment.Segment.ImageURL
			if imageURL == "" {
				log.Printf("Image URL is empty for segment %d", idx+1)
				errChan <- fmt.Errorf("image URL is empty for segment %d", idx+1)
				return
			}

			// Download the image
			resp, err := http.Get(imageURL)
			if err != nil {
				log.Printf("Error downloading image for segment %d: %v", idx+1, err)
				errChan <- fmt.Errorf("error downloading image for segment %d: %w", idx+1, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				log.Printf("Failed to download image for segment %d: Status %s", idx+1, resp.Status)
				errChan <- fmt.Errorf("failed to download image for segment %d: status %s", idx+1, resp.Status)
				return
			}

			// Save the image to a temporary file
			imageExt := filepath.Ext(imageURL)
			if imageExt == "" {
				imageExt = ".jpg" // Default extension if none found
			}
			localImagePath := filepath.Join(tempDir, fmt.Sprintf("segment_%d%s", idx+1, imageExt))
			out, err := os.Create(localImagePath)
			if err != nil {
				log.Printf("Error creating local image file for segment %d: %v", idx+1, err)
				errChan <- fmt.Errorf("error creating local image file for segment %d: %w", idx+1, err)
				return
			}
			defer out.Close()

			_, err = io.Copy(out, resp.Body)
			if err != nil {
				log.Printf("Error saving image for segment %d: %v", idx+1, err)
				errChan <- fmt.Errorf("error saving image for segment %d: %w", idx+1, err)
				return
			}

			// Use the local image path instead of the URL
			imagePath := localImagePath

			// Generate TTS audio for the segment text and get duration
			audioPath, audioDuration, err := getTTS(segment.Segment.Segment, idx+1, tempDir)
			if err != nil {
				log.Printf("Error generating TTS for segment %d: %v", idx+1, err)
				errChan <- err
				return
			}
			story.Segments[idx].AudioPath = audioPath

			// Temporary video path for the segment
			segmentVideoPath := filepath.Join(tempDir, fmt.Sprintf("segment_%d.mp4", idx+1))
			segmentVideos[idx] = segmentVideoPath

			// Calculate the number of frames for the segment
			segmentFrames := int(audioDuration * float64(frameRate))

			// Construct the filter complex
			filterComplex := fmt.Sprintf(
				"[0]scale=1344:-2,setsar=1:1[out];[out]crop=1344:768[out];[out]scale=8000:-1,zoompan=z='zoom+0.0005':x=iw/2-(iw/zoom/2):y=ih/2-(ih/zoom/2):d=%d:s=1344x768:fps=%d[out]",
				segmentFrames, frameRate,
			)

			// FFmpeg command to create a video for the segment with a smooth zoom
			ffmpegCmd := exec.Command("ffmpeg",
				"-y", // Overwrite without asking
				"-loop", "1",
				"-i", imagePath,
				"-filter_complex", filterComplex,
				"-c:v", "libx264",
				"-t", fmt.Sprintf("%.2f", audioDuration),
				"-pix_fmt", "yuv420p",
				"-r", fmt.Sprintf("%d", frameRate), // Set output frame rate
				"-map", "[out]",
				segmentVideoPath,
			)

			// Capture stderr for debugging
			var stderr bytes.Buffer
			ffmpegCmd.Stderr = &stderr

			log.Printf("Creating segment video: %s", segmentVideoPath)
			if err := ffmpegCmd.Run(); err != nil {
				log.Printf("FFmpeg error for segment %d: %v, Details: %s", idx+1, err, stderr.String())
				errChan <- err
				return
			}

			// Merge audio with the video segment
			mergedVideoPath := filepath.Join(tempDir, fmt.Sprintf("segment_%d_with_audio.mp4", idx+1))
			ffmpegMergeCmd := exec.Command("ffmpeg",
				"-y",
				"-i", segmentVideoPath,
				"-i", audioPath,
				"-c:v", "copy",
				"-c:a", "aac",
				"-shortest",
				mergedVideoPath,
			)

			var stderrMerge bytes.Buffer
			ffmpegMergeCmd.Stderr = &stderrMerge

			log.Printf("Merging audio into segment video: %s", mergedVideoPath)
			if err := ffmpegMergeCmd.Run(); err != nil {
				log.Printf("FFmpeg merge error for segment %d: %v, Details: %s", idx+1, err, stderrMerge.String())
				errChan <- err
				return
			}

			// Update the segmentVideos slice with the merged video path
			segmentVideos[idx] = mergedVideoPath
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
		segmentBase := filepath.Base(segmentVideo)
		concatFile.WriteString(fmt.Sprintf("file '%s'\n", segmentBase))
	}

	// Create the final video by concatenating all segment videos
	videoPath := filepath.Join(tempDir, fmt.Sprintf("story_%d_video.mp4", story.ID))
	ffmpegConcatCmd := exec.Command("ffmpeg",
		"-y",           // Overwrite without asking
		"-f", "concat", // Use concat demuxer
		"-safe", "0", // Allow unsafe file paths
		"-i", concatListPath, // Input concat list
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-fps_mode", "cfr", // Enforce constant frame rate
		"-r", fmt.Sprintf("%d", frameRate), // Set output frame rate
		videoPath, // Output video path
	)

	// Capture stderr for debugging
	var stderrConcat bytes.Buffer
	ffmpegConcatCmd.Stderr = &stderrConcat

	log.Printf("Concatenating segment videos into final video: %s", videoPath)
	if err := ffmpegConcatCmd.Run(); err != nil {
		log.Printf("FFmpeg concat error: %v, Details: %s", err, stderrConcat.String())
		return "", err
	}

	log.Printf("Video created successfully at %s", videoPath)
	// Upload video to R2
	videoFile, err := os.Open(videoPath)
	if err != nil {
		log.Printf("Error opening video file: %v", err)
		return "", fmt.Errorf("error opening video file: %w", err)
	}
	defer videoFile.Close()

	if middleware.R2Client == nil {
		return "", errors.New("r2 client is not initialized")
	}

	objectKey := fmt.Sprintf("videos/story_%d_video.mp4", storyID)
	_, err = middleware.R2Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String("halloween"),
		Key:         aws.String(objectKey),
		Body:        videoFile,
		ContentType: aws.String("video/mp4"),
	})
	if err != nil {
		log.Printf("Error uploading video to R2: %v", err)
		return "", fmt.Errorf("error uploading video to R2: %w", err)
	}

	r2VideoURL := fmt.Sprintf("%s/%s", os.Getenv("R2_S3_API"), objectKey)

	// Update the story with the video URL
	if err := database.DB.Model(&models.Story{}).Where("id = ?", storyID).Update("video_url", r2VideoURL).Error; err != nil {
		log.Printf("Error updating story with VideoURL: %v", err)
		return "", fmt.Errorf("error updating story with VideoURL: %w", err)
	}

	// Clean up temporary segment videos and concat file after final video creation
	for _, segmentVideo := range segmentVideos {
		if err := os.Remove(segmentVideo); err != nil {
			log.Printf("Warning: Failed to remove temporary file %s: %v", segmentVideo, err)
		}
	}
	if err := os.Remove(concatListPath); err != nil {
		log.Printf("Warning: Failed to remove concat list file %s: %v", concatListPath, err)
	}
	if err := os.Remove(videoPath); err != nil {
		log.Printf("Warning: Failed to remove final video file %s: %v", videoPath, err)
	}

	log.Printf("Video uploaded to R2 and story updated with URL: %s", r2VideoURL)
	return r2VideoURL, nil
}

func getTTS(text string, idx int, tempDir string) (string, float64, error) {
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

	outputFile := filepath.Join(tempDir, fmt.Sprintf("speech_%d.mp3", idx))
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
