//go:build ignore

package main

import (
	"context"
	"fmt"
	"os"

	oai "github.com/openai/openai-go"
	tolvyn "github.com/tolvyn/tolvyn-go"
	tolvynopenai "github.com/tolvyn/tolvyn-go/openai"
)

func main() {
	client := tolvynopenai.NewClient(tolvyn.ClientOptions{
		TolvynAPIKey:   os.Getenv("TOLVYN_API_KEY"),
		ProviderAPIKey: os.Getenv("OPENAI_API_KEY"),
		Team:           "engineering",
		Service:        "streaming-example",
		FailOpen:       true,
	})

	stream := client.Chat.Completions.NewStreaming(context.Background(),
		oai.ChatCompletionNewParams{
			Model: oai.F(oai.ChatModelGPT4oMini),
			Messages: oai.F([]oai.ChatCompletionMessageParamUnion{
				oai.ChatCompletionUserMessageParam{
					Role: oai.F(oai.ChatCompletionUserMessageParamRoleUser),
					Content: oai.F([]oai.ChatCompletionContentPartUnionParam{
						oai.ChatCompletionContentPartTextParam{
							Text: oai.F("Count to 5, one number per word."),
							Type: oai.F(oai.ChatCompletionContentPartTextTypeText),
						},
					}),
				},
			}),
		},
	)

	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			fmt.Print(chunk.Choices[0].Delta.Content)
		}
	}
	fmt.Println()

	if err := stream.Err(); err != nil {
		fmt.Printf("Stream error: %v\n", err)
		os.Exit(1)
	}
}
