// Command models prints the IDs currently exposed by the OpenCode Go catalog.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	opencodeauth "github.com/mattsp1290/opencode-auth-go"
)

func main() {
	// NewClient still requires OPENCODE_GO_API_KEY because it can also issue
	// authenticated inference requests. ListModels itself sends no credential.
	client, err := opencodeauth.NewClient(opencodeauth.Options{
		UserAgent: "opencode-auth-go-models-example/1.0",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "unable to configure OpenCode Go client")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models, err := client.ListModels(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "unable to list OpenCode Go models")
		os.Exit(1)
	}
	for _, model := range models {
		fmt.Println(model.ID)
	}
}
