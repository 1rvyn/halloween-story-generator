package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/1rvyn/halloween-story-generator/database"
	"github.com/1rvyn/halloween-story-generator/middleware"
	"github.com/1rvyn/halloween-story-generator/models"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gofiber/fiber/v2"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Choice struct {
	Index   int     `json:"index"`
	Message Message `json:"message"`
}

type GroqAPIResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

var replicateResp struct {
	Output []string `json:"output"`
	Error  string   `json:"error"`
	Status string   `json:"status"`
	URLs   struct {
		Get string `json:"get"`
	} `json:"urls"`
}

func groqReuest(story models.Story) (string, error) {
	groqReq := models.GroqRequest{
		Messages: []models.Message{
			{Role: "system", Content: models.StorySegmentationInstance.Prompt},
			{Role: "user", Content: story.Content},
		},
		Model:       "llama-3.1-70b-versatile",
		Temperature: 1,
		MaxTokens:   1024,
		TopP:        1,
		Stream:      false, // Changed from true to false
		Stop:        nil,
	}

	reqBody, err := json.Marshal(groqReq)
	if err != nil {
		log.Printf("Error marshalling request: %v", err)
		return "", err
	}

	req, err := http.NewRequest("POST",
		"https://api.groq.com/openai/v1/chat/completions",
		bytes.NewBuffer(reqBody))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return "", err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("GROQ_API_KEY")))
	req.Header.Set("Content-Type", "application/json")
	// **Edit Ends Here**

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error making request: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return "", err
	}

	log.Printf("Response body: %s", body)

	var apiResp GroqAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		log.Printf("Error unmarshalling Groq API response: %v", err)
		return "", err
	}

	if len(apiResp.Choices) == 0 {
		log.Printf("No choices found in Groq response")
		return "", err
	}

	// Extract the content which contains the XML segments
	xmlContent := apiResp.Choices[0].Message.Content
	return xmlContent, nil
}

func cleanAndSegmentXML(xmlContent string, storyID int) ([]models.Segment, error) {
	segmentRegex := regexp.MustCompile(`<segment number="(\d+)">\s*([\s\S]*?)\s*</segment>`)
	matches := segmentRegex.FindAllStringSubmatch(xmlContent, -1)
	if matches == nil {
		log.Printf("No segments found in Groq response")
		return nil, errors.New("no segments found in Groq response")
	}

	segments := []models.Segment{}

	for _, match := range matches {
		if len(match) < 3 {
			log.Printf("Unexpected match format: %v", match)
			continue
		}
		segNumberStr := match[1]
		segNumber, err := strconv.Atoi(segNumberStr)
		if err != nil {
			log.Printf("Invalid segment number: %v", err)
			continue
		}
		segContent := match[2]
		segmentContent := strings.TrimSpace(segContent)
		if segmentContent == "" {
			continue
		}

		// Create Segment record
		segment := models.Segment{
			StoryID: storyID,
			Segment: segmentContent,
			Number:  segNumber,
		}

		if err := database.DB.Create(&segment).Error; err != nil {
			log.Printf("Error creating segment: %v", err)
			continue
		}

		segments = append(segments, segment)
	}

	return segments, nil
}

func CreateStory(c *fiber.Ctx) error {
	story := new(models.Story)
	if err := c.BodyParser(story); err != nil {
		log.Printf("Error parsing JSON: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot parse JSON",
		})
	}

	// Retrieve the authenticated user from JWT
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}
	story.CreatedBy = int(user.ID)
	database.DB.Create(story).Scan(&story)
	fmt.Printf("Just created story ID: %d\n", story.ID)

	// created story
	xmlContent, err := groqReuest(*story)
	if err != nil {
		log.Printf("Error in groq request: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}
	// cleanup xml to segment
	segments, err := cleanAndSegmentXML(xmlContent, int(story.ID))
	if err != nil {
		log.Printf("Error in cleanAndSegmentXML: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	fmt.Printf("Parsed %v total segments:", len(segments))

	for _, segment := range segments {
		err := processSegment(segment, int(story.ID))
		if err != nil {
			log.Printf("Error processing segment %d: %v", segment.Number, err)
			continue
		}
	}

	// Generate ffmpeg input file with durations
	inputFilePath := fmt.Sprintf("temp/story_%d_input.txt", story.ID)
	inputFile, err := os.Create(inputFilePath)
	if err != nil {
		log.Printf("Error creating ffmpeg input file: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Video creation failed",
		})
	}
	defer inputFile.Close()

	for _, segment := range segments {
		inputFile.WriteString(fmt.Sprintf("file '%s'\n", segment.ImageURL))
		inputFile.WriteString(fmt.Sprintf("duration %.2f\n", segment.Duration))
	}

	// Specify the last image without duration
	if len(segments) > 0 {
		lastSegment := segments[len(segments)-1]
		inputFile.WriteString(fmt.Sprintf("file '%s'\n", lastSegment.ImageURL))
	}

	// Create video using ffmpeg with dynamic durations
	videoPath := fmt.Sprintf("temp/story_%d_video.mp4", story.ID)
	ffmpegCmd := exec.Command("ffmpeg",
		"-f", "concat",
		"-safe", "0",
		"-i", inputFilePath,
		"-c:v", "libx264",
		"-r", "30",
		"-pix_fmt", "yuv420p",
		videoPath,
	)

	// Capture stdout and stderr
	var stderr bytes.Buffer
	ffmpegCmd.Stderr = &stderr

	if err := ffmpegCmd.Run(); err != nil {
		log.Printf("FFmpeg error: %v, Details: %s", err, stderr.String())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Video creation failed",
		})
	}

	// Upload video to R2
	videoFile, err := os.Open(videoPath)
	if err != nil {
		log.Printf("Error opening video file: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}
	defer videoFile.Close()

	objectKey := fmt.Sprintf("videos/story_%d.mp4", story.ID)
	_, err = middleware.R2Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String("halloween"),
		Key:         aws.String(objectKey),
		Body:        videoFile,
		ContentType: aws.String("video/mp4"),
	})
	if err != nil {
		log.Printf("Error uploading video to R2: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Video upload failed",
		})
	}

	r2VideoURL := fmt.Sprintf("%s/%s", os.Getenv("R2_S3_API"), objectKey)
	story.VideoURL = r2VideoURL

	if err := database.DB.Save(&story).Error; err != nil {
		log.Printf("Error updating story with VideoURL: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	// Clean up temporary files
	os.Remove(inputFilePath)
	os.Remove(videoPath)

	log.Printf("Story created with ID: %d and video URL: %s", story.ID, r2VideoURL)
	return c.Status(fiber.StatusCreated).JSON(story)
}

// new function to handle replicate requests and image processing
func processSegment(segment models.Segment, storyID int) error {
	replicatePayload := map[string]interface{}{
		"input": map[string]interface{}{
			"prompt":         segment.Segment,
			"num_outputs":    1,
			"aspect_ratio":   "1:1",
			"output_format":  "webp",
			"output_quality": 100,
		},
	}

	replicateBody, err := json.Marshal(replicatePayload)
	if err != nil {
		return fmt.Errorf("marshalling Replicate request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.replicate.com/v1/models/black-forest-labs/flux-schnell/predictions", bytes.NewBuffer(replicateBody))
	if err != nil {
		return fmt.Errorf("creating Replicate request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("REPLICATE_API_TOKEN")))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("making Replicate request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("replicate api error: %s", string(bodyBytes))
	}

	replicateRespBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading Replicate response: %w", err)
	}

	// parse replicate response to get prediction URLs

	if err := json.Unmarshal(replicateRespBody, &replicateResp); err != nil {
		return fmt.Errorf("unmarshalling Replicate response: %w", err)
	}

	if replicateResp.Status != "succeeded" && replicateResp.Status != "failed" {
		// Poll the get URL until status is succeeded or failed
		maxRetries := 20
		retryInterval := 1 * time.Second
		for i := 0; i < maxRetries; i++ {
			time.Sleep(retryInterval)

			req, err := http.NewRequest("GET", replicateResp.URLs.Get, nil)
			if err != nil {
				log.Printf("Error creating poll request: %v", err)
				continue
			}

			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("REPLICATE_API_TOKEN")))
			req.Header.Set("Content-Type", "application/json")

			getResp, err := client.Do(req)
			if err != nil {
				log.Printf("Error polling Replicate URL: %v", err)
				continue
			}

			getBody, err := io.ReadAll(getResp.Body)
			getResp.Body.Close()
			if err != nil {
				log.Printf("Error reading poll response: %v", err)
				continue
			}

			var pollResp struct {
				Output []string `json:"output"`
				Error  string   `json:"error"`
				Status string   `json:"status"`
			}
			if err := json.Unmarshal(getBody, &pollResp); err != nil {
				log.Printf("Error unmarshalling poll response: %v", err)
				continue
			}

			log.Printf("Polling attempt %d: Status=%s", i+1, pollResp.Status)

			if pollResp.Status == "succeeded" {
				replicateResp.Output = pollResp.Output
				break
			} else if pollResp.Status == "failed" {
				replicateResp.Error = pollResp.Error
				break
			}
		}
	}

	if len(replicateResp.Output) == 0 {
		if replicateResp.Error != "" {
			return fmt.Errorf("replicate api error for segment %d: %s", segment.Number, replicateResp.Error)
		}
		return fmt.Errorf("no output from replicate for segment %d", segment.Number)
	}

	imageURL := replicateResp.Output[0]

	// download image from replicate and upload to r2
	if middleware.R2Client == nil {
		return errors.New("r2 client is not initialized")
	}

	imageResp, err := http.Get(imageURL)
	if err != nil {
		return fmt.Errorf("downloading image: %w", err)
	}
	defer imageResp.Body.Close()

	imageData, err := io.ReadAll(imageResp.Body)
	if err != nil {
		return fmt.Errorf("reading image data: %w", err)
	}

	objectKey := fmt.Sprintf("images/story_%d_segment_%d.webp", storyID, segment.Number)

	_, err = middleware.R2Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String("halloween"),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(imageData),
		ContentType: aws.String("image/webp"),
	})
	if err != nil {
		return fmt.Errorf("uploading to R2: %w", err)
	}

	r2ImageURL := fmt.Sprintf("%s/%s", os.Getenv("R2_S3_API"), objectKey)
	segment.ImageURL = r2ImageURL

	// Calculate duration based on text length
	wordCount := len(strings.Fields(segment.Segment))
	wordsPerSecond := 2.5 // Average speaking rate
	duration := float64(wordCount) / wordsPerSecond
	segment.Duration = duration

	if err := database.DB.Save(&segment).Error; err != nil {
		return fmt.Errorf("updating segment with ImageURL and Duration: %w", err)
	}

	return nil
}

func GetStories(c *fiber.Ctx) error {
	// Retrieve the authenticated user from JWT
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		log.Printf("User not found in locals")
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}

	fmt.Println("User ID reading stories:", user.ID)

	var stories []models.Story
	if err := database.DB.Where("created_by = ?", user.ID).Find(&stories).Error; err != nil {
		log.Printf("Error fetching stories: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	log.Printf("Fetched %d stories for user ID %d", len(stories), user.ID)
	return c.JSON(stories)
}
