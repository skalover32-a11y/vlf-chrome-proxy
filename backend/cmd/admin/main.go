package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/app"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/repository"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/tokens"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: admin <create-access-link|list-nodes>")
	}

	ctx := context.Background()
	runtime, err := app.New(ctx)
	if err != nil {
		log.Fatalf("start runtime: %v", err)
	}
	defer runtime.Close()

	switch os.Args[1] {
	case "create-access-link":
		if err := createAccessLink(ctx, runtime, os.Args[2:]); err != nil {
			log.Fatalf("create access link: %v", err)
		}
	case "list-nodes":
		if err := listNodes(ctx, runtime); err != nil {
			log.Fatalf("list nodes: %v", err)
		}
	default:
		log.Fatalf("unknown command: %s", os.Args[1])
	}
}

func createAccessLink(ctx context.Context, runtime *app.Runtime, args []string) error {
	fs := flag.NewFlagSet("create-access-link", flag.ExitOnError)
	label := fs.String("label", "bootstrap", "human-readable label")
	source := fs.String("source", "manual", "access link source")
	defaultNodeID := fs.String("default-node", "", "default node id")
	nodes := fs.String("nodes", "", "comma-separated node ids")
	expiresIn := fs.String("expires-in", "720h", "expiration duration")
	ifEmpty := fs.Bool("if-empty", false, "create only when there are no access links")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *ifEmpty {
		count, err := runtime.Repo.CountAccessLinks(ctx)
		if err != nil {
			return err
		}
		if count > 0 {
			fmt.Println("access links already exist, skipping bootstrap creation")
			return nil
		}
	}

	duration, err := time.ParseDuration(*expiresIn)
	if err != nil {
		return fmt.Errorf("parse expires-in: %w", err)
	}

	tokenManager := tokens.NewManager(runtime.Config.TokenPepper, runtime.Config.ProxyPasswordPepper)
	rawToken, tokenHash, err := tokenManager.NewAccessToken()
	if err != nil {
		return err
	}

	nodesList := parseCSV(*nodes)
	if *defaultNodeID == "" && len(nodesList) > 0 {
		*defaultNodeID = nodesList[0]
	}

	link, err := runtime.Repo.CreateAccessLink(ctx, repository.CreateAccessLinkParams{
		TokenHash:      tokenHash,
		Label:          *label,
		Source:         *source,
		AllowedNodeIDs: nodesList,
		DefaultNodeID:  *defaultNodeID,
		ExpiresAt:      time.Now().UTC().Add(duration),
	})
	if err != nil {
		return err
	}

	fullURL := runtime.Service.AccessLinkBaseURL() + "/access/" + rawToken
	fmt.Printf("access_link_id=%s\n", link.ID)
	fmt.Printf("access_link_url=%s\n", fullURL)
	fmt.Printf("expires_at=%s\n", link.ExpiresAt.Format(time.RFC3339))
	return nil
}

func listNodes(ctx context.Context, runtime *app.Runtime) error {
	nodes, err := runtime.Repo.ListNodes(ctx)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		fmt.Printf("%s\t%s\t%s:%d\t%s\tdefault=%t\n", node.ID, node.Name, node.Host, node.ProxyPort, node.Status, node.IsDefault)
	}
	return nil
}

func parseCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}
