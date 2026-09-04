// Command samizdatimport mirrors the FULL Substack history of
// consciousrepository.com into the vault as a local-authored `samizdat`
// collection — one note per post, images downloaded content-addressed into
// attachments/, uniform frontmatter (owner decisions 2026-09-04, plan
// system/workbench/plans/2026-09-04-samizdat-mirror-and-authoring.md).
//
// A note is named after the post's TITLE the way the owner names notes —
// lowercase, spaces between words, apostrophes kept, no dashes
// ("100 days with visualize value's daily manifest.md") — not after the URL
// slug. The URL slug stays the post's identity for -only and the page cache.
//
// It is repeatable and safe:
//   - enumeration is the SITEMAP (the RSS feed exposes only the latest 20);
//     with the sitemap unavailable it falls back to feed.xml + the archive API.
//   - every post is re-fetched from its live page (the canonical source) and
//     the note is REPLACED from source — re-running updates in place, never
//     creates duplicates.
//   - images land in attachments/<sha256>.<ext>; an existing hash file is
//     reused, never rewritten.
//   - an existing top-level note that duplicates a post is MOVED to archive/
//     (never deleted); intrinsic/ is never read or touched.
//   - every vault write goes through vaultwriter with a declared capability
//     and lands in the write-audit log.
//
// Usage: go run ./cmd/samizdatimport [-vault /private/consciousrepo] [-dry-run]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"manifest/record"
	"manifest/vaultwriter"
)

const (
	siteBase = "https://www.consciousrepository.com"

	capNotes       = "samizdat-notes"
	capAttachments = "samizdat-attachments"
	capArchive     = "samizdat-archive"
)

// report is the run summary printed at the end (and as JSON with -json).
type report struct {
	Enumerated      int      `json:"enumerated"`
	Imported        int      `json:"imported"`
	Unchanged       int      `json:"unchanged"`
	Written         int      `json:"written"`
	ImagesSeen      int      `json:"imagesSeen"`
	ImagesDownload  int      `json:"imagesDownloaded"`
	ImagesReused    int      `json:"imagesReused"`
	ImagesFailed    int      `json:"imagesFailed"`
	Archived        []string `json:"archived"`
	Failed          []string `json:"failed"`
	NearMisses      []string `json:"nearMisses"`
	DryRun          bool     `json:"dryRun"`
	EnumerationFrom string   `json:"enumerationFrom"`
}

func main() {
	vaultPath := flag.String("vault", defaultVault(), "path to the Obsidian vault")
	folder := flag.String("folder", "samizdat", "vault-relative folder for the mirrored notes")
	attachDir := flag.String("attachments", "attachments", "vault-relative folder for content-addressed images")
	archiveDir := flag.String("archive", "archive", "vault-relative folder that receives deduped originals")
	cacheDir := flag.String("cache", "", "optional dir of cached page HTML (<slug>.html); misses are fetched and stored")
	dryRun := flag.Bool("dry-run", false, "fetch + convert but write nothing to the vault")
	only := flag.String("only", "", "comma-separated slugs to import (default: all)")
	delay := flag.Duration("delay", 1500*time.Millisecond, "pause between requests to the site (it rate-limits)")
	jsonOut := flag.Bool("json", false, "print the final report as JSON")
	systemRoot := flag.String("system", "system", "system-zone root")
	extrinsicRoot := flag.String("extrinsic", "extrinsic", "extrinsic-zone root")
	flag.Parse()

	if *vaultPath == "" {
		fmt.Fprintln(os.Stderr, "usage: samizdatimport -vault <path> [-dry-run] [-only slug,slug]")
		os.Exit(2)
	}
	if st, err := os.Stat(*vaultPath); err != nil || !st.IsDir() {
		log.Fatalf("vault %q is not a directory", *vaultPath)
	}
	log.SetFlags(log.Ltime)

	// §A3 write boundary: three knowledge-zone capabilities, one per folder
	// this tool may touch. Nothing else in the vault is writable from here.
	home, _ := os.UserHomeDir()
	vw := vaultwriter.New(*vaultPath).
		WithZoneRoots(*systemRoot, *extrinsicRoot).
		WithAudit(filepath.Join(home, ".config", "manifest")).
		Grant(
			vaultwriter.Capability{Name: capNotes, Zone: record.ZoneKnowledge,
				Pattern: strings.Trim(*folder, "/") + "/**", Actor: vaultwriter.ActorUserAction},
			vaultwriter.Capability{Name: capAttachments, Zone: record.ZoneKnowledge,
				Pattern: strings.Trim(*attachDir, "/") + "/**", Actor: vaultwriter.ActorUserAction},
			vaultwriter.Capability{Name: capArchive, Zone: record.ZoneKnowledge,
				Pattern: strings.Trim(*archiveDir, "/") + "/**", Actor: vaultwriter.ActorUserAction},
		)

	ctx := context.Background()
	f := newFetcher(*delay, *cacheDir)
	store := &vaultStore{
		vw: vw, vault: *vaultPath, folder: strings.Trim(*folder, "/"),
		attachments: strings.Trim(*attachDir, "/"), archive: strings.Trim(*archiveDir, "/"),
		dryRun: *dryRun,
	}

	var rep report
	rep.DryRun = *dryRun

	entries, from, err := enumerate(ctx, f)
	if err != nil {
		log.Fatalf("enumerate: %v", err)
	}
	rep.EnumerationFrom = from
	if *only != "" {
		want := map[string]bool{}
		for _, s := range strings.Split(*only, ",") {
			want[strings.TrimSpace(s)] = true
		}
		var kept []siteEntry
		for _, e := range entries {
			if want[e.Slug] {
				kept = append(kept, e)
			}
		}
		entries = kept
	}
	rep.Enumerated = len(entries)
	log.Printf("enumerated %d posts (%s)", len(entries), from)

	// Existing top-level notes that could be duplicates of a post — scanned
	// once, matched per post. intrinsic/ and every subfolder are excluded.
	candidates, err := scanCandidates(*vaultPath)
	if err != nil {
		log.Fatalf("scan candidates: %v", err)
	}
	log.Printf("dedupe: %d top-level writing/published notes to match against", len(candidates))
	matched := map[string]bool{}

	for i, e := range entries {
		log.Printf("[%d/%d] %s", i+1, len(entries), e.Slug)
		page, err := f.page(ctx, e)
		if err != nil {
			log.Printf("  FAIL fetch: %v", err)
			rep.Failed = append(rep.Failed, e.Slug)
			continue
		}
		post, err := extract(page, e)
		if err != nil {
			log.Printf("  FAIL extract: %v", err)
			rep.Failed = append(rep.Failed, e.Slug)
			continue
		}
		// Images: download content-addressed, then rewrite the markdown.
		for _, im := range post.Images {
			rep.ImagesSeen++
			local, status, err := store.saveImage(ctx, f, im)
			switch {
			case err != nil:
				rep.ImagesFailed++
				log.Printf("  image FAIL %s: %v", short(im.Original), err)
				continue // the markdown keeps the remote URL for this one
			case status == imgReused:
				rep.ImagesReused++
			default:
				rep.ImagesDownload++
			}
			post.Markdown = strings.ReplaceAll(post.Markdown, "("+im.Src+")", "("+local+")")
		}
		note := renderNote(post)
		// The note is named after the TITLE (lowercase, spaces, no dashes).
		// A note already holding this post's URL under another name — an
		// older naming rule, or a post re-titled since — is updated in place.
		name, kept := store.noteFor(noteName(post.Title, e.Slug), post.URL, e.URL)
		if kept {
			log.Printf("  keeping existing note %s/%s.md (title now %q)", store.folder, name, post.Title)
		}
		wrote, err := store.writeNote(name, note)
		if err != nil {
			log.Printf("  FAIL write: %v", err)
			rep.Failed = append(rep.Failed, e.Slug)
			continue
		}
		rep.Imported++
		if wrote {
			rep.Written++
			log.Printf("  wrote %s/%s.md (%d images, published %s)", store.folder, name, len(post.Images), post.Published)
		} else {
			rep.Unchanged++
			log.Printf("  unchanged %s/%s.md", store.folder, name)
		}
		// Dedupe: an existing top-level note that is this post → archive/.
		for _, c := range candidates {
			if matched[c.Name] || !c.matches(post) {
				continue
			}
			matched[c.Name] = true
			dest, err := store.archiveNote(c.Name)
			if err != nil {
				log.Printf("  FAIL archive %q: %v", c.Name, err)
				continue
			}
			rep.Archived = append(rep.Archived, c.Name+" -> "+dest)
			log.Printf("  archived duplicate %q -> %s", c.Name, dest)
		}
	}
	for _, c := range candidates {
		if !matched[c.Name] {
			rep.NearMisses = append(rep.NearMisses, c.Name)
		}
	}
	sort.Strings(rep.NearMisses)

	if *jsonOut {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
		return
	}
	fmt.Println()
	fmt.Println("== samizdat import report ==")
	if rep.DryRun {
		fmt.Println("DRY RUN — nothing written")
	}
	fmt.Printf("enumerated:        %d (%s)\n", rep.Enumerated, rep.EnumerationFrom)
	fmt.Printf("imported:          %d (written %d, unchanged %d)\n", rep.Imported, rep.Written, rep.Unchanged)
	fmt.Printf("images:            %d seen, %d downloaded, %d reused (dedupe), %d failed\n",
		rep.ImagesSeen, rep.ImagesDownload, rep.ImagesReused, rep.ImagesFailed)
	fmt.Printf("archived dupes:    %d\n", len(rep.Archived))
	for _, a := range rep.Archived {
		fmt.Printf("  %s\n", a)
	}
	fmt.Printf("failed posts:      %d\n", len(rep.Failed))
	for _, s := range rep.Failed {
		fmt.Printf("  %s\n", s)
	}
	fmt.Printf("unmatched writing/published notes (audit by hand): %d\n", len(rep.NearMisses))
	for _, s := range rep.NearMisses {
		fmt.Printf("  %s\n", s)
	}
	if len(rep.Failed) > 0 {
		os.Exit(1)
	}
}

// defaultVault reads vaultPath from the repo's config.json when present so the
// tool runs bare from the repo root; -vault always overrides.
func defaultVault() string {
	for _, p := range []string{"config.json", filepath.Join("..", "config.json")} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg struct {
			VaultPath string `json:"vaultPath"`
		}
		if json.Unmarshal(b, &cfg) == nil && cfg.VaultPath != "" {
			return cfg.VaultPath
		}
	}
	return ""
}

func short(s string) string {
	if len(s) > 90 {
		return s[:87] + "..."
	}
	return s
}
