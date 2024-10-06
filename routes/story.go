package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/1rvyn/halloween-story-generator/database"
	"github.com/1rvyn/halloween-story-generator/middleware"
	"github.com/1rvyn/halloween-story-generator/misc"
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

	log.Printf("Groq Response body: \n%s\n", body)

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

	// Retrieve the authenticated user_id from Locals
	userID, ok := c.Locals("user_id").(uint)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}
	story.CreatedBy = int(userID)

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

	err = replicateRequests(segments, int(story.ID))
	if err != nil {
		log.Printf("Error processing segments: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	// Generate video using the segments
	videoFilePath, err := misc.GenerateFfmpegInputFile(int(story.ID), segments)
	if err != nil {
		log.Printf("Error generating video: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Video creation failed",
		})
	}

	// Upload video to R2
	now := time.Now()
	videoFile, err := os.Open(videoFilePath)
	elapsed := time.Since(now)
	if err != nil {
		log.Printf("Error opening video file (took %v): %v", elapsed, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to open video file",
		})
	}
	defer videoFile.Close()
	log.Printf("Opening video file took: %v", elapsed)

	if middleware.R2Client == nil {
		log.Printf("R2 client is not initialized")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	objectKey := fmt.Sprintf("videos/story_%d_video.mp4", story.ID)
	_, err = middleware.R2Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String("halloween"),
		Key:         aws.String(objectKey),
		Body:        videoFile,
		ContentType: aws.String("video/mp4"),
	})
	if err != nil {
		log.Printf("Error uploading video to R2: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to upload video",
		})
	}

	r2VideoURL := fmt.Sprintf("%s/%s", os.Getenv("R2_S3_API"), objectKey)

	// Update the story with the video URL
	if err := database.DB.Model(&models.Story{}).Where("id = ?", story.ID).Update("video_url", r2VideoURL).Error; err != nil {
		log.Printf("Error updating story with VideoURL: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update story with video URL",
		})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"videoURL": r2VideoURL,
	})
}

func replicateRequests(segments []models.Segment, storyID int) error {
	var wg sync.WaitGroup
	var mutex sync.Mutex
	errChan := make(chan error, len(segments))

	for i := range segments {
		wg.Add(1)
		go func(seg *models.Segment) {
			defer wg.Done()
			log.Printf("Starting processing for segment %d", seg.Number)
			// Prepare the payload for Replicate API
			replicatePayload := map[string]interface{}{
				"input": map[string]interface{}{
					"prompt":         seg.Segment,
					"num_outputs":    1,
					"aspect_ratio":   "16:9",
					"output_format":  "webp",
					"output_quality": 20,
				},
			}

			replicateBody, err := json.Marshal(replicatePayload)
			if err != nil {
				errChan <- fmt.Errorf("marshalling Replicate request: %w", err)
				return
			}

			req, err := http.NewRequest("POST", "https://api.replicate.com/v1/models/black-forest-labs/flux-schnell/predictions", bytes.NewBuffer(replicateBody))
			if err != nil {
				errChan <- fmt.Errorf("creating Replicate request: %w", err)
				return
			}

			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("REPLICATE_API_TOKEN")))
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				errChan <- fmt.Errorf("making Replicate request: %w", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusCreated {
				bodyBytes, _ := io.ReadAll(resp.Body)
				errChan <- fmt.Errorf("replicate API error: %s", string(bodyBytes))
				return
			}

			replicateRespBody, err := io.ReadAll(resp.Body)
			if err != nil {
				errChan <- fmt.Errorf("reading Replicate response: %w", err)
				return
			}

			var pollResp struct {
				Output []string `json:"output"`
				Error  string   `json:"error"`
				Status string   `json:"status"`
				URLs   struct { // Correctly nested URLs object
					Get string `json:"get"`
				} `json:"urls"`
			}
			if err := json.Unmarshal(replicateRespBody, &pollResp); err != nil {
				errChan <- fmt.Errorf("unmarshalling Replicate response: %w", err)
				return
			}

			// Use the URL from the initial response for polling
			pollingURL := pollResp.URLs.Get
			if pollingURL == "" {
				errChan <- fmt.Errorf("no polling URL provided in the initial response")
				return
			}

			// Polling until the prediction is succeeded or failed
			for pollResp.Status != "succeeded" && pollResp.Status != "failed" {
				time.Sleep(1 * time.Second)

				getReq, err := http.NewRequest("GET", pollingURL, nil)
				if err != nil {
					errChan <- fmt.Errorf("creating poll request: %w", err)
					return
				}

				getReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("REPLICATE_API_TOKEN")))
				getReq.Header.Set("Content-Type", "application/json")

				getResp, err := client.Do(getReq)
				if err != nil {
					errChan <- fmt.Errorf("polling Replicate URL: %w", err)
					return
				}

				getBody, err := io.ReadAll(getResp.Body)
				getResp.Body.Close()
				if err != nil {
					errChan <- fmt.Errorf("reading poll response: %w", err)
					return
				}

				if err := json.Unmarshal(getBody, &pollResp); err != nil {
					errChan <- fmt.Errorf("unmarshalling poll response: %w", err)
					return
				}

				log.Printf("Polling for segment %d: Status=%s", seg.Number, pollResp.Status)
			}

			if pollResp.Status != "succeeded" {
				errChan <- fmt.Errorf("replicate API failed for segment %d: %s", seg.Number, pollResp.Error)
				return
			}

			if len(pollResp.Output) == 0 {
				errChan <- fmt.Errorf("no output from Replicate for segment %d", seg.Number)
				return
			}

			imageURL := pollResp.Output[0]

			// Download the image from Replicate
			imageResp, err := http.Get(imageURL)
			if err != nil {
				errChan <- fmt.Errorf("downloading image for segment %d: %w", seg.Number, err)
				return
			}
			defer imageResp.Body.Close()

			imageData, err := io.ReadAll(imageResp.Body)
			if err != nil {
				errChan <- fmt.Errorf("reading image data for segment %d: %w", seg.Number, err)
				return
			}

			// Upload the image to R2
			objectKey := fmt.Sprintf("images/story_%d_segment_%d.webp", storyID, seg.Number)
			_, err = middleware.R2Client.PutObject(context.TODO(), &s3.PutObjectInput{
				Bucket:      aws.String("halloween"),
				Key:         aws.String(objectKey),
				Body:        bytes.NewReader(imageData),
				ContentType: aws.String("image/webp"),
			})
			if err != nil {
				errChan <- fmt.Errorf("uploading to R2 for segment %d: %w", seg.Number, err)
				return
			}

			r2ImageURL := fmt.Sprintf("%s/%s", os.Getenv("R2_S3_API"), objectKey)

			// Update the segment with the R2 Image URL
			mutex.Lock()
			seg.ImageURL = r2ImageURL
			if err := database.DB.Save(seg).Error; err != nil {
				mutex.Unlock()
				errChan <- fmt.Errorf("updating segment %d with ImageURL: %w", seg.Number, err)
				return
			}
			mutex.Unlock()

			// Store image data in memory for ffmpeg processing
			mutex.Lock()
			seg.ImageData = imageData
			mutex.Unlock()

		}(&segments[i])
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var combinedErr error
	for err := range errChan {
		if combinedErr == nil {
			combinedErr = err
		} else {
			combinedErr = fmt.Errorf("%v; %w", combinedErr, err)
		}
	}

	return combinedErr
}

func GetStories(c *fiber.Ctx) error {
	// Retrieve the authenticated user_id from Locals
	userID, ok := c.Locals("user_id").(uint)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}

	fmt.Println("User ID reading stories:", userID)

	var stories []models.Story
	if err := database.DB.Where("created_by = ?", userID).Find(&stories).Error; err != nil {
		log.Printf("Error fetching stories: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	log.Printf("Fetched %d stories for user ID %d", len(stories), userID)
	return c.JSON(stories)
}
