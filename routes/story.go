package routes

import (
	"bytes"
	"context"
	"encoding/json" // Ensure xml is imported for XML parsing
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv" // Add strconv for string to int conversion
	"strings"
	"time"

	"github.com/1rvyn/halloween-story-generator/database"
	"github.com/1rvyn/halloween-story-generator/middleware"
	"github.com/1rvyn/halloween-story-generator/models"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gofiber/fiber/v2"
)

func CreateStory(c *fiber.Ctx) error {
	story := new(models.Story)
	if err := c.BodyParser(story); err != nil {
		log.Printf("Error parsing JSON: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot parse JSON",
		})
	}

	// insert and read id
	database.DB.Create(story).Scan(&story)
	fmt.Printf("Story ID: %d\n", story.ID)

	userID, ok := c.Locals("user_id").(uint)
	if !ok {
		log.Printf("User ID not found in locals")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	story.CreatedBy = int(userID)

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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	req, err := http.NewRequest("POST",
		"https://api.groq.com/openai/v1/chat/completions",
		bytes.NewBuffer(reqBody))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("GROQ_API_KEY")))
	req.Header.Set("Content-Type", "application/json")
	// **Edit Ends Here**

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error making request: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	log.Printf("Response body: %s", body)

	// **Parse Groq Response into Segments Starts Here**

	// Define structs to parse the Groq API JSON response
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

	var apiResp GroqAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		log.Printf("Error unmarshalling Groq API response: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Invalid Groq response format",
		})
	}

	if len(apiResp.Choices) == 0 {
		log.Printf("No choices found in Groq response")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Invalid Groq response",
		})
	}

	// Extract the content which contains the XML segments
	xmlContent := apiResp.Choices[0].Message.Content

	// Apply the custom escape function

	fmt.Println("XML content:", xmlContent)

	segmentRegex := regexp.MustCompile(`<segment number="(\d+)">\s*([\s\S]*?)\s*</segment>`)
	matches := segmentRegex.FindAllStringSubmatch(xmlContent, -1)
	if matches == nil {
		log.Printf("No segments found in Groq response")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Invalid Groq response",
		})
	}

	for _, match := range matches {
		if len(match) < 3 {
			log.Printf("Unexpected match format: %v", match)
			continue
		}
		segNumberStr := match[1]
		segNumber, err := strconv.Atoi(segNumberStr) // Convert segment number to int
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
			StoryID: int(story.ID),
			Segment: segmentContent,
			Number:  segNumber, // Now assigns an int value
		}

		if err := database.DB.Create(&segment).Error; err != nil {
			log.Printf("Error creating segment: %v", err)
			continue
		}

		fmt.Println("Segment content for story:", story.ID, "segment:", segNumber, "content:", segmentContent)

		// **Generate Image for Segment Starts Here**

		// Prepare Replicate API request payload
		replicatePayload := map[string]interface{}{
			"input": map[string]interface{}{
				"prompt":         segmentContent,
				"num_outputs":    1,
				"aspect_ratio":   "1:1",
				"output_format":  "webp",
				"output_quality": 100,
			},
		}

		replicateBody, err := json.Marshal(replicatePayload)
		if err != nil {
			log.Printf("Error marshalling Replicate request: %v", err)
			continue
		}

		req, err := http.NewRequest("POST", "https://api.replicate.com/v1/models/black-forest-labs/flux-schnell/predictions", bytes.NewBuffer(replicateBody))
		if err != nil {
			log.Printf("Error creating Replicate request: %v", err)
			continue
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("REPLICATE_API_TOKEN")))
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Error making Replicate request: %v", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			bodyBytes, _ := ioutil.ReadAll(resp.Body)
			log.Printf("Replicate API error: %s", string(bodyBytes))
			continue
		}

		replicateRespBody, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading Replicate response: %v", err)
			continue
		}

		// Parse Replicate response to get prediction URLs
		var replicateResp struct {
			Output []string `json:"output"`
			Error  string   `json:"error"`
			Status string   `json:"status"`
			URLs   struct {
				Get string `json:"get"`
			} `json:"urls"`
		}
		if err := json.Unmarshal(replicateRespBody, &replicateResp); err != nil {
			log.Printf("Error unmarshalling Replicate response: %v", err)
			continue
		}

		if replicateResp.Status != "succeeded" && replicateResp.Status != "failed" {
			// Poll the get URL until status is succeeded or failed
			maxRetries := 20 // Increased from 10 to allow more time
			retryInterval := 1 * time.Second
			for i := 0; i < maxRetries; i++ {
				time.Sleep(retryInterval)

				req, err := http.NewRequest("GET", replicateResp.URLs.Get, nil)
				if err != nil {
					log.Printf("Error creating request for polling Replicate get URL: %v", err)
					continue
				}

				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("REPLICATE_API_TOKEN")))
				req.Header.Set("Content-Type", "application/json")

				client := &http.Client{}
				getResp, err := client.Do(req)
				if err != nil {
					log.Printf("Error polling Replicate get URL: %v", err)
					continue
				}

				getBody, err := io.ReadAll(getResp.Body)
				getResp.Body.Close()
				if err != nil {
					log.Printf("Error reading Replicate poll response: %v", err)
					continue
				}

				var pollResp struct {
					Output []string `json:"output"`
					Error  string   `json:"error"`
					Status string   `json:"status"`
				}
				if err := json.Unmarshal(getBody, &pollResp); err != nil {
					log.Printf("Error unmarshalling Replicate poll response: %v", err)
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
				// Continue polling if status is still pending or starting
			}
		}

		// **Update Condition to Check Output Instead of URLs.Get**
		if len(replicateResp.Output) == 0 {
			if replicateResp.Error != "" {
				log.Printf("Replicate API error for segment %d: %s", segNumber, replicateResp.Error)
			} else {
				log.Printf("No output from Replicate for segment %d", segNumber)
			}
			continue
		}

		imageURL := replicateResp.Output[0]

		// **Download Image from Replicate and Upload to R2 Starts Here**

		// Ensure R2Client is initialized
		if middleware.R2Client == nil {
			log.Println("R2Client is not initialized")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Internal Server Error",
			})
		}

		// Download the image
		imageResp, err := http.Get(imageURL)
		if err != nil {
			log.Printf("Error downloading image from Replicate: %v", err)
			continue
		}
		defer imageResp.Body.Close()

		imageData, err := io.ReadAll(imageResp.Body)
		if err != nil {
			log.Printf("Error reading image data: %v", err)
			continue
		}

		// Define the object key for R2
		objectKey := fmt.Sprintf("images/story_%d_segment_%d.webp", story.ID, segNumber)

		// Upload to R2
		_, err = middleware.R2Client.PutObject(context.TODO(), &s3.PutObjectInput{
			Bucket:      aws.String("halloween"), // Ensure this matches your R2 bucket name
			Key:         aws.String(objectKey),
			Body:        bytes.NewReader(imageData),
			ContentType: aws.String("image/webp"),
		})
		if err != nil {
			log.Printf("Error uploading image to R2: %v", err)
			continue
		}

		// Construct the R2 image URL
		r2ImageURL := fmt.Sprintf("%s/%s", os.Getenv("R2_S3_API"), objectKey)
		segment.ImageURL = r2ImageURL

		// Update Segment with ImageURL
		if err := database.DB.Save(&segment).Error; err != nil {
			log.Printf("Error updating segment with ImageURL: %v", err)
			continue
		}
	}

	// **Parse Groq Response into Segments Ends Here**

	log.Printf("Story created with ID: %d", story.ID)
	return c.Status(fiber.StatusCreated).JSON(story)
}

func GetStories(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uint)
	if !ok {
		log.Printf("User ID not found in locals")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	fmt.Println("User ID reading stories:", userID)

	var stories []models.Story
	if err := database.DB.Find(&stories).Error; err != nil {
		log.Printf("Error fetching stories: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	log.Printf("Fetched %d stories", len(stories))
	return c.JSON(stories)
}
