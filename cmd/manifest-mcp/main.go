package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"manifest/manifestmcp"
	"os"
)

func main() {
	config := flag.String("config", "config.json", "Manifest configuration path (read only)")
	catalog := flag.Bool("catalog", false, "emit generated catalog without reading config or domain files")
	readme := flag.Bool("readme", false, "emit generated README")
	flag.Parse()
	if *readme {
		b, err := manifestmcp.README()
		if err != nil {
			fail(err)
		}
		fmt.Print(string(b))
		return
	}
	if *catalog {
		b, err := manifestmcp.Catalog()
		if err != nil {
			fail(err)
		}
		fmt.Print(string(b))
		return
	}
	b, err := os.ReadFile(*config)
	if err != nil {
		fail(err)
	}
	var c struct{ VaultPath, DataDir, SystemRoot string }
	if err = json.Unmarshal(b, &c); err != nil {
		fail(err)
	}
	if c.SystemRoot == "" {
		c.SystemRoot = "system"
	}
	if c.VaultPath == "" || c.DataDir == "" {
		fail(fmt.Errorf("config requires vaultPath and dataDir"))
	}
	a, err := manifestmcp.New(c.VaultPath, c.DataDir, c.SystemRoot)
	if err != nil {
		fail(err)
	}
	if err = a.Server().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
