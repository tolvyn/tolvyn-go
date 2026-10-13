package main

import (
	"context"
	"fmt"
	"os"

	sdk "github.com/anthropics/anthropic-sdk-go"
	tolvyn "github.com/tolvyn/tolvyn-go"
	tolvynanthropic "github.com/tolvyn/tolvyn-go/anthropic"
)

func main() {
	client := tolvynanthropic.NewClient(tolvyn.ClientOptions{
		TolvynAPIKey:   os.Getenv("TOLVYN_API_KEY"),
		ProviderAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		Team:           "engineering",
		Service:        "example",
		// FALLBACK IS ON BY DEFAULT. The option is DisableFailOpen, so the
		// zero value enables it -- there is deliberately no field of the name
		// this example used to set. It needs ProviderAPIKey above: without one
		// there is no fallback and the request fails rather than going direct.
		// Requests that DO go direct never reach TOLVYN, so they are NOT
		// metered and will not appear in the customer's costs.
	})

	msg, err := client.Messages.New(context.Background(), sdk.MessageNewParams{
		Model:     sdk.F(sdk.ModelClaude3_5HaikuLatest),
		MaxTokens: sdk.F(int64(256)),
		Messages: sdk.F([]sdk.MessageParam{
			sdk.NewUserMessage(sdk.NewTextBlock("Say: TOLVYN Go SDK works!")),
		}),
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if len(msg.Content) > 0 {
		fmt.Println(msg.Content[0].Text)
	}
}
