// Command aion-collab-migrate repairs pre-stable-id collaboration during the
// transition release. Normal current-item mappings are automatic at startup;
// the archive flags are for an exactly identified item deleted before cutover.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"manifest/aion"
	"manifest/teamportal"
)

func main() {
	vault := flag.String("vault", "", "vault root")
	root := flag.String("aion-root", "system/aion", "Aion root relative to the vault")
	teamDir := flag.String("team-dir", "", "portal team-state directory")
	actor := flag.String("actor", "manifest-migration@aion.bio", "migration actor")
	archiveID := flag.String("archive-id", "", "known deleted legacy item id to archive")
	archiveTitle := flag.String("archive-title", "", "exact historical title for -archive-id")
	archiveKind := flag.String("archive-kind", "task", "historical kind")
	archiveOwner := flag.String("archive-owner", "", "historical owner")
	archiveCaptured := flag.String("archive-captured", "", "historical captured date")
	archiveStatus := flag.String("archive-status", "open", "historical status")
	flag.Parse()
	if *vault == "" || *teamDir == "" {
		fmt.Fprintln(os.Stderr, "-vault and -team-dir are required")
		os.Exit(2)
	}

	store := aion.NewStore(*vault, *root, func(string, []byte) error {
		return errors.New("read-only migration store")
	})
	team, err := teamportal.New(*teamDir)
	if err != nil {
		fatal(err)
	}
	identity := teamportal.Identity{Email: *actor, Name: "Manifest migration"}
	now := time.Now()
	migrated, err := team.MigrateItemIDs(store.LegacyIDMap(), identity, now)
	if err != nil {
		fatal(err)
	}
	archived := false
	if *archiveID != "" {
		if *archiveTitle == "" {
			fatal(errors.New("-archive-title is required with -archive-id"))
		}
		err = team.Archive(identity, teamportal.ArchivedItem{
			ID: *archiveID, Kind: *archiveKind, Title: *archiveTitle,
			Owner: *archiveOwner, Captured: *archiveCaptured,
			Status: *archiveStatus,
		}, now)
		if err != nil {
			fatal(err)
		}
		archived = true
	}
	fmt.Printf("migrated=%d archived=%t\n", migrated, archived)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
