package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

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
		Stream:      true,
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

	// Assuming the Groq API returns XML with multiple <segment> tags

	var groqResp models.GroqResponse
	if err := xml.Unmarshal(body, &groqResp); err != nil {
		log.Printf("Error unmarshalling Groq response: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Invalid Groq response",
		})
	}

	for _, seg := range groqResp.Segments {
		segmentContent := strings.TrimSpace(seg.Content)
		if segmentContent == "" {
			continue
		}

		// Create Segment record
		segment := models.Segment{
			StoryID: int(story.ID),
			Segment: segmentContent,
		}

		if err := database.DB.Create(&segment).Error; err != nil {
			log.Printf("Error creating segment: %v", err)
			continue
		}

		fmt.Println("Segment content for story:", story.ID, "segment:", seg.Number, "content:", segmentContent)

		// **Generate Image for Segment Starts Here**

		// Prepare Replicate API request payload
		replicatePayload := map[string]interface{}{
			"input": map[string]interface{}{
				"prompt":         fmt.Sprintf("%s, tasty, food photography, dynamic shot", segmentContent),
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

		replicateRespBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading Replicate response: %v", err)
			continue
		}

		// Parse Replicate response to get image URL
		var replicateResp struct {
			Output []string `json:"output"`
		}
		if err := json.Unmarshal(replicateRespBody, &replicateResp); err != nil {
			log.Printf("Error unmarshalling Replicate response: %v", err)
			continue
		}

		if len(replicateResp.Output) == 0 {
			log.Printf("No output from Replicate for segment %s", seg.Number)
			continue
		}

		imageURL := replicateResp.Output[0]

		// **Download Image from Replicate and Upload to R2 Starts Here**

		// Download the image
		imageResp, err := http.Get(imageURL)
		if err != nil {
			log.Printf("Error downloading image from Replicate: %v", err)
			continue
		}
		defer imageResp.Body.Close()

		imageData, err := ioutil.ReadAll(imageResp.Body)
		if err != nil {
			log.Printf("Error reading image data: %v", err)
			continue
		}

		// Define the object key for R2
		objectKey := fmt.Sprintf("images/story_%d_segment_%s.webp", story.ID, seg.Number)

		// Upload to R2
		_, err = middleware.R2Client.PutObject(context.TODO(), &s3.PutObjectInput{
			Bucket:      aws.String("halloween"), // Replace with your bucket name if different
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
