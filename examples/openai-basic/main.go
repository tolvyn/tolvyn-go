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
		Service:        "example",
		// FALLBACK IS ON BY DEFAULT. The option is DisableFailOpen, so the
		// zero value enables it -- there is deliberately no field of the name
		// this example used to set. It needs ProviderAPIKey above: without one
		// there is no fallback and the request fails rather than going direct.
		// Requests that DO go direct never reach TOLVYN, so they are NOT
		// metered and will not appear in the customer's costs.
	})

	resp, err := client.Chat.Completions.New(context.Background(),
		oai.ChatCompletionNewParams{
			Model: oai.F(oai.ChatModelGPT4oMini),
			Messages: oai.F([]oai.ChatCompletionMessageParamUnion{
				oai.ChatCompletionUserMessageParam{
					Role: oai.F(oai.ChatCompletionUserMessageParamRoleUser),
					Content: oai.F([]oai.ChatCompletionContentPartUnionParam{
						oai.ChatCompletionContentPartTextParam{
							Text: oai.F("Say: TOLVYN Go SDK works!"),
							Type: oai.F(oai.ChatCompletionContentPartTextTypeText),
						},
					}),
				},
			}),
		},
	)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(resp.Choices[0].Message.Content)
}
