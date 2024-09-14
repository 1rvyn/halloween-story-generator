package models

type Prompt struct {
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
	Source string `json:"source"`
}

var StorySegmentation = Prompt{
	Name: "Story Segmentation",
	Prompt: `You are tasked with segmenting a scary story into smaller parts that can be used for narration in a video. Each segment should correspond to a distinct part of the story that could be paired with a single image or scene in the video. Here's how to approach this task:

1. First, you will be given a scary story in the following format:

<story>
{{STORY}}
</story>

2. Your job is to split this story into logical segments that can be narrated separately while showing a related image.

3. When creating segments, follow these guidelines:
   - Each segment should be a coherent part of the story, typically 2-4 sentences long.
   - Segments should end at natural breaking points in the narrative.
   - Try to keep segments roughly similar in length, but prioritize narrative coherence over strict length equality.
   - Ensure that each segment could theoretically be paired with a single, static image that represents its content.

4. Format your output as follows:
   - Number each segment sequentially.
   - Enclose each segment in <segment> tags.
   - Include the segment number as an attribute of the segment tag.

5. Here's an example of how your output should look:

<segment number="1">
It was a dark and stormy night. Sarah heard a strange noise coming from the attic. She tried to ignore it, but the sound kept getting louder.
</segment>

<segment number="2">
Gathering her courage, Sarah decided to investigate. She grabbed a flashlight and slowly climbed the creaky stairs to the attic. The door was slightly ajar.
</segment>

6. Now, please process the provided story and create appropriate segments. Remember to consider the narrative flow and how each segment might be represented visually in a video.`,
	Source: "Anthropic",
}
