// re-intake-shadow replays one embedded redacted fixture. It never boots the
// Manifest server, accesses a harness or launches a model/provider process.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"manifest/hermes"
	"manifest/reintake"
	"os"
)

func main() {
	config := flag.String("config", "", "explicit offline config file (no defaults)")
	fixture := flag.String("fixture", "", "single or split; exactly one document per replay")
	flag.Parse()
	if *config == "" || flag.NArg() != 0 {
		fail()
	}
	b, err := os.ReadFile(*config)
	if err != nil {
		fail()
	}
	var cfg struct {
		DataDir  string          `json:"dataDir"`
		ReIntake reintake.Config `json:"reIntake"`
		Hermes   struct {
			Duties map[string]hermes.DutyAuthority `json:"duties"`
		} `json:"hermes"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		fail()
	}
	report, err := reintake.Replay(cfg.DataDir, cfg.ReIntake, cfg.Hermes.Duties, *fixture)
	if err != nil {
		fail()
	}
	fmt.Printf("%s: structured parity=%t; prose semantic-review-only; live usage unverified; not routed\n", report.Status, report.StructuredParity)
}
func fail() { fmt.Fprintln(os.Stderr, reintake.StopAndPage); os.Exit(1) }
