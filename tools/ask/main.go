// Command ask sends one message to an A2A agent and prints the answer.
//
//	go run ./tools/ask "Triage ticket #1"
//	go run ./tools/ask -agent http://localhost:8002 "Account ACC-1042, requester billing@bluefin.example.com: ..."
//
// Task states go to stderr and the agent's answer to stdout.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
)

func main() {
	agentURL := flag.String("agent", envOr("AGENT_URL", "http://localhost:8001"), "base URL of the agent (its card is at /.well-known/agent-card.json)")
	timeout := flag.Duration("timeout", 10*time.Minute, "how long to wait for the answer")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: ask [-agent URL] [-timeout D] <message>\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	text := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if text == "" {
		flag.Usage()
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := ask(ctx, *agentURL, text); err != nil {
		fmt.Fprintln(os.Stderr, "ask:", err)
		os.Exit(1)
	}
}

func ask(ctx context.Context, agentURL, text string) error {
	card, err := agentcard.DefaultResolver.Resolve(ctx, agentURL)
	if err != nil {
		return fmt.Errorf("fetch agent card from %s: %w", agentURL, err)
	}
	fmt.Fprintf(os.Stderr, "→ %s (%s)\n", card.Name, agentURL)
	if err := useCardHost(card, agentURL); err != nil {
		return err
	}

	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		return fmt.Errorf("create A2A client: %w", err)
	}
	defer func() { _ = client.Destroy() }()

	req := &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(text))}
	printed := false
	for event, err := range client.SendStreamingMessage(ctx, req) {
		if err != nil {
			return err
		}
		switch e := event.(type) {
		case *a2a.Message:
			printed = printParts(e.Parts) || printed
		case *a2a.TaskArtifactUpdateEvent:
			printed = printParts(e.Artifact.Parts) || printed
		case *a2a.TaskStatusUpdateEvent:
			fmt.Fprintf(os.Stderr, "[%s]\n", e.Status.State)
			if e.Status.Message != nil && e.Status.State != a2a.TaskStateWorking {
				printed = printParts(e.Status.Message.Parts) || printed
			}
			if e.Status.State == a2a.TaskStateFailed {
				return fmt.Errorf("task failed")
			}
		case *a2a.Task:
			if e.Status.State.Terminal() && !printed {
				for _, art := range e.Artifacts {
					printed = printParts(art.Parts) || printed
				}
			}
		}
	}
	if !printed {
		return fmt.Errorf("the agent sent no text")
	}
	return nil
}

// useCardHost points the card's endpoints at the host the card was fetched
// from. Inside Docker an agent advertises its service name, such as
// http://billing-agent:8002, which does not resolve from the host machine.
func useCardHost(card *a2a.AgentCard, agentURL string) error {
	base, err := url.Parse(agentURL)
	if err != nil {
		return err
	}
	for _, iface := range card.SupportedInterfaces {
		u, err := url.Parse(iface.URL)
		if err != nil {
			return fmt.Errorf("agent card has a bad URL %q: %w", iface.URL, err)
		}
		if u.Host != base.Host {
			u.Scheme, u.Host = base.Scheme, base.Host
			iface.URL = u.String()
		}
	}
	return nil
}

func printParts(parts a2a.ContentParts) bool {
	printed := false
	for _, p := range parts {
		if t := p.Text(); t != "" {
			fmt.Println(t)
			printed = true
		}
	}
	return printed
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
