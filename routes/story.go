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

	userID, ok := c.Locals("user_id").(uint)
	if !ok {
		log.Printf("User ID not found in locals")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal Server Error",
		})
	}

	story.CreatedBy = int(userID)
	database.DB.Create(story).Scan(&story)
	fmt.Printf("Story ID: %d\n", story.ID)

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

	fmt.Printf("Parsed %v segments:", len(segments))

	for _, segment := range segments {
		err := processSegment(segment, int(story.ID))
		if err != nil {
			log.Printf("Error processing segment %d: %v", segment.Number, err)
			continue
		}
	}

	log.Printf("Story created with ID: %d", story.ID)
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

	if err := database.DB.Save(&segment).Error; err != nil {
		return fmt.Errorf("updating segment with ImageURL: %w", err)
	}

	return nil
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
