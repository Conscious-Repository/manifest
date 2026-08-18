// Command manifest serves the local daily-planner dashboard over an Obsidian vault.
//
// Architectural invariant (see obsidian-as-database.md): the app is read-only on the
// knowledge vault. The ONLY writes into the vault are the user's own note saves through
// explicit dashboard actions (daily notes, goals) — never AI-authored content, never in
// the background. All derived/operational state (the calendar cache, and the read-only
// index to come) lives under cfg.DataDir, OUTSIDE the vault, and is disposable/rebuildable.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/calendar"
	"manifest/capture"
	"manifest/chatthreads"
	"manifest/contacts"
	"manifest/daily"
	"manifest/errands"
	"manifest/fundraising"
	"manifest/geocode"
	"manifest/gmailauth"
	"manifest/goals"
	"manifest/hermes"
	"manifest/ledger"
	"manifest/portals"
	"manifest/reading"
	"manifest/realestate"
	"manifest/record"
	"manifest/server"
	"manifest/signals"
	"manifest/spirits"
	"manifest/tasks"
	"manifest/teamportal"
	"manifest/threads"
	"manifest/vault"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	vaultFlag := flag.String("vault", "", "override vault path from config")
	port := flag.Int("port", 0, "override port from config")
	portalPort := flag.Int("portal-port", 0, "override the standalone AION portal port from config")
	deriveAgentKey := flag.String("derive-agent-key", "", "print the FILES agent key for a host (run on the box holding the master) and exit")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("loading config %s: %v", *configPath, err)
	}
	if *vaultFlag != "" {
		cfg.VaultPath = expandHome(*vaultFlag)
	}
	if *port != 0 {
		cfg.Port = *port
	}
	if *portalPort != 0 {
		cfg.PortalPort = *portalPort
	}
	if *deriveAgentKey != "" {
		// mint the per-host FILES agent key from this box's master (P8):
		// installed once on the device; the master never ships.
		key, err := server.DeriveAgentKeyFromMaster(filepath.Join(cfg.DataDir, "agent_master"), *deriveAgentKey)
		if err != nil {
			log.Fatalf("derive agent key: %v", err)
		}
		fmt.Println(key)
		return
	}
	if cfg.VaultPath == "" {
		fmt.Fprintln(os.Stderr, "error: vaultPath is not set. Edit config.json or pass -vault /path/to/vault")
		os.Exit(1)
	}
	if fi, err := os.Stat(cfg.VaultPath); err != nil || !fi.IsDir() {
		log.Fatalf("vault path %q is not a readable directory: %v", cfg.VaultPath, err)
	}
	// Hard invariant: the app never writes derived data into the vault. All
	// derived/operational state (the calendar cache, and the read-only index next)
	// lives under DataDir, which must therefore sit OUTSIDE the vault.
	if pathIsUnder(cfg.DataDir, cfg.VaultPath) {
		log.Fatalf("dataDir %q must live outside the vault %q (derived data never goes in the vault)", cfg.DataDir, cfg.VaultPath)
	}
	// Zone line (system-root-plan §1): warn (not fail) when the system folder is
	// absent — the zone model still applies; the folder appears with first use.
	if fi, err := os.Stat(filepath.Join(cfg.VaultPath, filepath.FromSlash(cfg.SystemRoot))); err != nil || !fi.IsDir() {
		log.Printf("system zone: %s/ not found in the vault yet (knowledge zone = whole vault until it exists)", cfg.SystemRoot)
	} else {
		log.Printf("system zone: %s/ (structured, app-managed markdown; knowledge zone = everything else)", cfg.SystemRoot)
	}

	idx, err := vault.NewIndex(vaultConfig(cfg))
	if err != nil {
		log.Fatalf("scanning vault: %v", err)
	}
	log.Printf("indexed %d daily notes (goals.md: %s)", len(idx.Dates()), orNone(idx.GoalsPath()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// extraction sinks (spec §3) — constructed BEFORE the watcher starts so
	// the reindex callback never races the wiring; nil without an engine.
	// Two domains share the one watcher: aion, and real-estate (categories
	// real-estate/ooda/ooda-group → the extractor spirit's real-estate ritual and the
	// system/realestate/backlog.md decision log). A note tagged for both
	// reaches both sinks — each files against its own record.
	var spiritsStore *spirits.Store
	var aionSink *aion.ExtractSink
	var reSink *aion.ExtractSink
	if cfg.ExcaliburPath != "" {
		spiritsStore = spirits.NewStore(cfg.ExcaliburPath).
			WithSkillsRoot(filepath.Join(cfg.VaultPath, "skills"))
		aionSink = aion.NewExtractSink(aion.ExtractorDomain, cfg.VaultPath, cfg.SystemRoot, cfg.ExtrinsicRoot, cfg.DataDir, spiritsStore)
		aionSink.Start(ctx)
		reSink = aion.NewExtractSink(aion.DomainSpec{
			Name:       "realestate",
			Categories: []string{"real-estate", "ooda", "ooda-group"},
			Spirit:     "extractor",
			Ritual:     "real-estate",
			Request:    "extract real-estate items from these vault notes:",
		}, cfg.VaultPath, cfg.SystemRoot, cfg.ExtrinsicRoot, cfg.DataDir, spiritsStore)
		reSink.Start(ctx)
	}
	// THE vault watcher (kernel-followups F2): one fsnotify descriptor set,
	// N sinks — the locator subscribes here, the content index below.
	watch, err := record.NewWatch(cfg.VaultPath, cfg.SkipDirs)
	if err != nil {
		log.Printf("file watcher disabled: %v", err)
		watch = nil
	} else {
		vault.NewWatcher(idx, vaultConfig(cfg), watch)
		defer watch.Close()
	}

	// Read-only headless-Dataview index over the WHOLE vault (the M0 kernel).
	// Derived/rebuildable → lives under DataDir, never the vault. It cold-builds
	// on first run and stays live via a background watcher; a build failure only
	// disables the contacts/query surfaces, never the core dashboard.
	vix, err := vaultindex.Open(vaultindex.Config{
		VaultRoot:     cfg.VaultPath,
		DBPath:        filepath.Join(cfg.DataDir, "index.db"),
		SystemRoot:    cfg.SystemRoot,
		ExtrinsicRoot: cfg.ExtrinsicRoot,
	})
	if err != nil {
		log.Printf("vault index disabled: %v", err)
		vix = nil
	} else {
		defer vix.Close()
		if n, err := vix.Rebuild(); err != nil {
			log.Printf("vault index build failed (contacts/query disabled): %v", err)
		} else {
			log.Printf("vault index: %d notes → %s", n, filepath.Join(cfg.DataDir, "index.db"))
		}
		if watch != nil {
			vix.SubscribeWatch(watch, 0, func(paths []string, err error) {
				if err != nil {
					log.Printf("vault reindex: %v", err)
					return
				}
				// extraction triggers (aion-domain spec §3): the sinks are
				// constructed later (they need the spirits store) — nil until then.
				if aionSink != nil {
					aionSink.Notify(paths)
				}
				if reSink != nil {
					reSink.Notify(paths)
				}
			})
		}
	}
	// both sinks registered — one descriptor set goes live
	if watch != nil {
		if err := watch.Start(ctx); err != nil {
			log.Printf("file watcher start failed: %v", err)
		}
	}

	// The §A3 write boundary: EVERY vault write — knowledge zone included, no
	// exemptions (owner decision 2026-07-28) — flows through a declared
	// vaultwriter capability and appends one line to <dataDir>/write-audit.log.
	vw := vaultwriter.New(cfg.VaultPath).
		WithZoneRoots(cfg.SystemRoot, cfg.ExtrinsicRoot).
		WithAudit(cfg.DataDir).
		Grant(
			// goals.md + "goals <quarter>.md" archives/reviews + .pre-* backups
			vaultwriter.Capability{Name: "goals", Zone: record.ZoneKnowledge,
				Pattern: strings.TrimSuffix(orDefault(cfg.GoalsFileName, "goals.md"), ".md") + "*",
				Actor:   vaultwriter.ActorUserAction},
			// tasks.md + "tasks archive.md" + .pre-* backups
			vaultwriter.Capability{Name: "todos", Zone: record.ZoneKnowledge,
				Pattern: strings.TrimSuffix(orDefault(cfg.TasksFileName, "tasks.md"), ".md") + "*",
				Actor:   vaultwriter.ActorUserAction},
			// daily notes: the exact YYYY-MM-DD.md shape, wherever they live
			vaultwriter.Capability{Name: "daily", Zone: record.ZoneKnowledge,
				Pattern: "????-??-??.md", Actor: vaultwriter.ActorUserAction},
			// AION — system/aion/** records; two actors, one subtree. The
			// approved-proposal capability is the §4 lane: ONLY the approvals
			// store's accept path writes under it (first live wiring of
			// ActorApprovedProposal).
			vaultwriter.Capability{Name: "aion", Zone: record.ZoneSystem,
				Pattern: filepath.ToSlash(filepath.Join(cfg.SystemRoot, "aion")) + "/**",
				Actor:   vaultwriter.ActorUserAction},
			vaultwriter.Capability{Name: "aion-approved", Zone: record.ZoneSystem,
				Pattern: filepath.ToSlash(filepath.Join(cfg.SystemRoot, "aion")) + "/**",
				Actor:   vaultwriter.ActorApprovedProposal},
			// PRIVATE FUNDRAISING CRM — explicitly separate from AION's public
			// live/export contract. Opportunity records and the shared note-less
			// contact registry have separately bounded capabilities.
			vaultwriter.Capability{Name: "fundraising", Zone: record.ZoneSystem,
				Pattern: filepath.ToSlash(filepath.Join(cfg.SystemRoot, "crm", "fundraising")) + "/**",
				Actor:   vaultwriter.ActorUserAction},
			vaultwriter.Capability{Name: "crm-contacts", Zone: record.ZoneSystem,
				Pattern: filepath.ToSlash(filepath.Join(cfg.SystemRoot, "crm", "contacts.md")),
				Actor:   vaultwriter.ActorUserAction},
			// REAL ESTATE — property `## todos` writes (redesign stage 4). The
			// declaration the direct re-* writers were always scheduled to get;
			// migrating those legacy guarded writers onto it is a later pass.
			vaultwriter.Capability{Name: "realestate", Zone: record.ZoneSystem,
				Pattern: filepath.ToSlash(filepath.Join(cfg.SystemRoot, "realestate")) + "/**",
				Actor:   vaultwriter.ActorUserAction},
			// the RE §4 approved-proposal lane: ONLY the approvals store's
			// accept path writes under it (re-backlog confirms)
			vaultwriter.Capability{Name: "realestate-approved", Zone: record.ZoneSystem,
				Pattern: filepath.ToSlash(filepath.Join(cfg.SystemRoot, "realestate")) + "/**",
				Actor:   vaultwriter.ActorApprovedProposal},
			// RE overhaul pass 2 — narrower names for audit granularity:
			// contract records (the committed-money source) and the CAS
			// document store (one blob per document, sha256-addressed)
			vaultwriter.Capability{Name: "re-contracts", Zone: record.ZoneSystem,
				Pattern: filepath.ToSlash(filepath.Join(cfg.SystemRoot, "realestate", "contracts")) + "/**",
				Actor:   vaultwriter.ActorUserAction},
			vaultwriter.Capability{Name: "re-files", Zone: record.ZoneSystem,
				Pattern: filepath.ToSlash(filepath.Join(cfg.SystemRoot, "realestate", "files")) + "/**",
				Actor:   vaultwriter.ActorUserAction},
			// TODO PANEL — system/todo-plans/** records (todo-panel plan D2/D6).
			// The owner's direct edits (description, plan) ride the user-action
			// capability; the agent-plan MATERIALIZATION rides the standing-
			// consent lane the §12 amendment (2026-08-15) grants.
			vaultwriter.Capability{Name: "todo-plans", Zone: record.ZoneSystem,
				Pattern: filepath.ToSlash(filepath.Join(cfg.SystemRoot, "todo-plans")) + "/**",
				Actor:   vaultwriter.ActorUserAction},
			vaultwriter.Capability{Name: "todo-plans-agent", Zone: record.ZoneSystem,
				Pattern: filepath.ToSlash(filepath.Join(cfg.SystemRoot, "todo-plans")) + "/**",
				Actor:   vaultwriter.ActorApprovedProposal},
			// AGENT PERSONAS — system/agents/personas/<intent>.md (persona plan
			// Phase 1): owner guidance to agents, seeded write-once, edited as
			// vault notes. Owner-confirmed home 2026-08-16: the system zone —
			// personas are guidance TO agents, never agent working state.
			vaultwriter.Capability{Name: "agent-personas", Zone: record.ZoneSystem,
				Pattern: filepath.ToSlash(filepath.Join(cfg.SystemRoot, "agents")) + "/**",
				Actor:   vaultwriter.ActorUserAction},
		)

	goalsStore := goals.NewStore(idx, cfg.VaultPath, cfg.GoalsFileName, vw.BindAbs("goals"))
	if err := goalsStore.Seed(); err != nil {
		log.Printf("seeding goals.md: %v", err)
	}
	// Silent one-time upgrade from the pre-v2 cascade to the horizon ladder; writes a
	// goals.md.pre-migration backup before its first migrated save (idempotent after).
	if migrated, err := goalsStore.Migrate(time.Now()); err != nil {
		log.Printf("migrating goals.md: %v", err)
	} else if migrated {
		log.Printf("goals.md migrated to the horizon ladder (backup: goals.md.pre-migration)")
	}

	// TODOS — the third surface over the vault-root `tasks.md` (peer of goals.md).
	tasksStore := tasks.NewStore(cfg.VaultPath, cfg.TasksFileName, vw.BindAbs("todos"))
	{
		var areaNames []string
		if doc := goalsStore.Load(); doc != nil {
			for _, a := range doc.Areas {
				areaNames = append(areaNames, a.Name)
			}
		}
		if migrated, err := tasksStore.Migrate(time.Now(), areaNames); err != nil {
			log.Printf("migrating %s: %v", cfg.TasksFileName, err)
		} else if migrated {
			log.Printf("%s migrated to the domain grammar (backup: %s.pre-migration)", cfg.TasksFileName, cfg.TasksFileName)
		}
		if n, err := tasksStore.Sweep(time.Now()); err == nil && n > 0 {
			log.Printf("todos: swept %d done item(s) to the archive", n)
		}
	}

	calClient := calendar.NewClient(ctx, cfg.Timezone)
	// Offline calendar mirror is derived data → lives under DataDir, never the vault.
	calSource := calendar.NewSource(calClient, filepath.Join(cfg.DataDir, "calendar-cache"))

	dc := dailyConfig(cfg)
	dc.Write = vw.BindAbs("daily")
	svc := daily.NewService(dc, idx)
	// aion store constructed early: the focus resolver offers a rock's open
	// aion backlog tasks alongside its tasks.md tethers (day task picker).
	aionRoot := filepath.ToSlash(filepath.Join(cfg.SystemRoot, "aion"))
	aionStore := aion.NewStore(cfg.VaultPath, aionRoot, vw.BindAbs("aion"))
	frRoot := filepath.ToSlash(filepath.Join(cfg.SystemRoot, "crm", "fundraising"))
	frRegistry := filepath.ToSlash(filepath.Join(cfg.SystemRoot, "crm", "contacts.md"))
	frStore := fundraising.NewStore(cfg.VaultPath, frRoot, frRegistry, vw.BindAbs("fundraising"), vw.BindAbs("crm-contacts"))
	if err := frStore.Ensure(); err != nil {
		log.Printf("fundraising CRM registry unavailable: %v", err)
	}
	// The real-estate decision log reuses the aion store/grammar pointed at
	// system/realestate — ONLY backlog methods are wired (server/re.go);
	// the other corpus methods must never touch this root.
	reRootRel := filepath.ToSlash(filepath.Join(cfg.SystemRoot, "realestate"))
	reStore := aion.NewStore(cfg.VaultPath, reRootRel, vw.BindAbs("realestate"))
	if _, err := os.Stat(filepath.Join(cfg.VaultPath, filepath.FromSlash(reRootRel), "backlog.md")); os.IsNotExist(err) {
		if _, err := vw.CreateRecord(reRootRel+"/backlog.md", aion.REBacklogSeed); err != nil {
			log.Printf("seeding real-estate backlog: %v", err)
		}
	}
	// assumptions.md: the single global underwriting set — seeded at the portal
	// defaults so Settings edits and the PUBLISH render have a real record
	// (reads fall back to defaults either way; the record is the edit surface).
	if _, err := os.Stat(filepath.Join(cfg.VaultPath, filepath.FromSlash(reRootRel), realestate.AssumptionsFile)); os.IsNotExist(err) {
		if _, err := vw.CreateRecord(reRootRel+"/"+realestate.AssumptionsFile, realestate.SeedAssumptions()); err != nil {
			log.Printf("seeding real-estate assumptions: %v", err)
		}
	}
	svc.UseGoals(server.NewGoalsAdapter(goalsStore, tasksStore, aionStore, reStore, orDefault(cfg.OwnerInitials, "BA")))
	svc.UseEvents(calSource)
	srv := server.New(svc, goalsStore, calClient)
	// One geocoder instance serves every feature so the provider's global rate
	// limit cannot be exceeded by contacts and properties independently.
	srv.UseGeocoder(geocode.New(cfg.DataDir))
	srv.UseFundraising(frStore)
	srv.UseTasks(tasksStore)
	srv.UseSticky(filepath.Join(cfg.DataDir, "sticky.md")) // ⌘I floating post-it (scratch, never the vault)
	srv.UseCapture(capture.NewStore(cfg.DataDir))          // the tray (cmd-ctr Stage; dataDir until promoted)
	srv.UseSTT(cfg.LabSttUrl, cfg.LabSttModel)             // mic dictation → lab granite-speech (P6)
	// TERMINAL: in-app PTY over tmux. Socket dir under dataDir so it's writable
	// under the metis systemd sandbox (TMUX_TMPDIR); cwd defaults to $HOME.
	{
		home, _ := os.UserHomeDir()
		srv.UseTerminal(filepath.Join(cfg.DataDir, "terminals.json"), filepath.Join(cfg.DataDir, "tmux"), home)
		// the cockpit's ssh fleet (device selector); self = this box's hostname
		devs := make([]server.TermDevice, 0, len(cfg.TerminalDevices))
		for _, d := range cfg.TerminalDevices {
			devs = append(devs, server.TermDevice(d))
		}
		self, _ := os.Hostname()
		if i := strings.IndexByte(self, '.'); i > 0 {
			self = self[:i]
		}
		srv.UseDevices(orDefault(self, "metis"), devs,
			filepath.Join(cfg.DataDir, "device_overrides.json"),
			filepath.Join(cfg.DataDir, "ssh_known_hosts"))
	}
	// FILES fleet browser (P8): local roots + tailnet agents; the agent-auth
	// master lives in dataDir and never leaves this box.
	{
		agents := map[string]string{}
		for _, a := range cfg.FilesAgents {
			agents[a.Name] = a.URL
		}
		host, _ := os.Hostname()
		if i := strings.IndexByte(host, '.'); i > 0 {
			host = host[:i]
		}
		srv.UseFiles(orDefault(host, "local"), cfg.FilesRoots, agents, filepath.Join(cfg.DataDir, "agent_master"))
	}
	// ACTIVITY: the cockpit's fleet vitals collector (needs devices+files wired)
	srv.UseActivity(filepath.Join(cfg.DataDir, "activity.json"))
	// Gmail read-only OAuth — manifest mints/validates the token the headless
	// excalibur engine reads for the ea-coordinator waiting-on digest, and
	// raises a FEED reconnect nudge when the sign-in expires.
	gmailClient := gmailauth.New()
	srv.UseGmail(gmailClient)
	srv.UseOwner(orDefault(cfg.OwnerInitials, "BA")) // "me" in the unified todo projection
	// ERRANDS — the action layer (errands-aside plan): records + transcripts
	// under <dataDir>/errands/ (never the vault); the aside CLI is the hands.
	// The store always exists (receipts render even with the CLI gone); runs
	// simply fail fast when the binary is absent.
	if estore, err := errands.NewStore(cfg.DataDir); err != nil {
		log.Printf("errands disabled: %v", err)
	} else {
		eexec := errands.NewExecutor(estore, cfg.ErrandTimeoutMinutes)
		eexec.Start()
		srv.UseErrands(estore, eexec)
		srv.SetErrandAccounts(cfg.ErrandAccounts)
		if errands.CLIPresent() {
			log.Printf("errands: enabled (aside CLI on PATH; timeout %dm)", max(cfg.ErrandTimeoutMinutes, 15))
		} else {
			log.Printf("errands: store ready, aside CLI not installed (portal row sealed)")
		}
	}
	var contactsSvc *contacts.Service // reused by the feed's cold-contact emitter
	var reSvc *realestate.Service     // reused by the feed's property emitters
	if vix != nil {
		srv.UseIndex(vix)
		// CONTACTS — the people layer over the index. Triage state lives under
		// DataDir (survives index rebuilds); calendar feeds upcoming-match (§6).
		cstore, err := contacts.NewStore(cfg.DataDir)
		if err != nil {
			log.Printf("contacts disabled: %v", err)
		} else {
			contactsSvc = contacts.New(vix, cstore, vw, calAdapter{calClient}, nil)
			srv.UseContacts(contactsSvc)
			log.Printf("contacts: enabled (people layer over the vault index)")
		}
		// READING — the book shelf over the extrinsic zone (reading-plan §3).
		srv.UseReading(reading.New(vix), cfg.ExtrinsicRoot)
		log.Printf("reading: enabled (book shelf over %s/)", cfg.ExtrinsicRoot)

		// PROPERTIES — the real-estate cockpit over system/realestate/ records.
		reRoot := filepath.ToSlash(filepath.Join(cfg.SystemRoot, "realestate"))
		reSvc = realestate.New(vix)
		srv.UseRealestate(reSvc, reRoot, cfg.DataDir)
		srv.UseRePortal(cfg.RePortalPath)
		// the CAS document store (overhaul §3.3) — capability-bound writes
		srv.UseREFiles(realestate.NewFileStore(cfg.VaultPath, reRoot, func(rel string, data []byte) error {
			return vw.WriteCap("re-files", rel, data)
		}))
		// Starter budget-mix template — write-once (goals.Seed precedent); the
		// user edits or adds templates as plain records forever after.
		if rel, err := vw.CreateRecord(reRoot+"/templates/gut-rehab.md", realestate.StarterTemplate); err == nil {
			_ = vix.ReindexPaths([]string{rel})
		}
		if rel, err := vw.CreateRecord(reRoot+"/templates/new-build.md", realestate.NewBuildTemplate); err == nil {
			_ = vix.ReindexPaths([]string{rel})
		}
		log.Printf("realestate: enabled (property records over %s/)", reRoot)
	}
	if cfg.FundraisingSheets.Enabled {
		backend, err := fundraising.NewGoogleSheetBackend(ctx, fundraising.GoogleSheetConfig{
			SpreadsheetID:   cfg.FundraisingSheets.SpreadsheetID,
			SheetID:         cfg.FundraisingSheets.SheetID,
			CredentialsPath: cfg.FundraisingSheets.CredentialsPath,
		})
		if err != nil {
			log.Printf("fundraising Sheets sync disabled: %v", err)
		} else {
			url := "https://docs.google.com/spreadsheets/d/" + cfg.FundraisingSheets.SpreadsheetID + "/edit#gid=" + fmt.Sprint(cfg.FundraisingSheets.SheetID)
			syncer := fundraising.NewSheetSync(frStore, backend, filepath.Join(cfg.DataDir, "fundraising", "sheet-sync.json"), url, srv.FundraisingSnapshot)
			srv.UseFundraisingSync(syncer)
			syncer.Start(ctx, time.Duration(cfg.FundraisingSheets.SyncIntervalMinutes)*time.Minute)
			log.Printf("fundraising Sheets sync: enabled (every %dm, gid %d)", cfg.FundraisingSheets.SyncIntervalMinutes, cfg.FundraisingSheets.SheetID)
		}
	}

	// AION — the program cockpit over system/aion/ records (aion-domain spec).
	// Seven corpora seeded write-once (valid of shape, empty of content —
	// except the owner-authored people roster); every write flows through the
	// "aion" capability.
	for _, name := range aion.Files {
		if rel, err := vw.CreateRecord(aionRoot+"/"+name, aion.SeedFiles[name]); err == nil && vix != nil {
			_ = vix.ReindexPaths([]string{rel})
		}
	}
	if n, collisions, err := aionStore.EnsureStableIDsFromPortal(cfg.AionPortal.Path); err != nil {
		log.Printf("aion stable-id migration failed: %v", err)
	} else if n > 0 {
		log.Printf("aion: assigned stable live-sync ids to %d backlog item(s) (%d collision(s) repaired)", n, collisions)
	}
	if cfg.AionPortal.Path != "" || cfg.AionPortal.Remote != "origin" || cfg.AionPortal.Branch != "main" {
		log.Printf("aionPortal.path/remote/branch are deprecated; path was read only for stable-ID migration and live sync ignores git coordinates")
	}
	srv.UseAion(aionStore, cfg.AionPortal.Path, cfg.AionPortal.Remote, cfg.AionPortal.Branch, cfg.DataDir)
	srv.UseRe(reStore)
	log.Printf("aion: enabled (program records over %s/)", aionRoot)

	// FEED SIGNALS — app-derived cards (going-cold contacts, stalled Rocks).
	// Computed at read time from state the dashboard already has; dismissals +
	// snoozes persist under DataDir (feed-signals.json), outside both trees.
	if sigStore, err := signals.NewStore(cfg.DataDir); err != nil {
		log.Printf("feed signals disabled: %v", err)
	} else {
		var emitters []signals.Emitter
		if contactsSvc != nil {
			emitters = append(emitters, signals.ColdContacts(contactsSvc))
		}
		emitters = append(emitters, signals.StalledRocks(goalsStore))
		emitters = append(emitters, signals.StaleTasks(tasksStore))
		emitters = append(emitters, signals.GmailReauth(gmailClient)) // "reconnect Gmail" nudge
		// manifest-sync's parked-conflict markers (big-change Phase 2b) — the
		// daemon writes <dataDir>/sync/<root>.conflict.json, deletes on resume
		emitters = append(emitters, signals.SyncConflicts(filepath.Join(cfg.DataDir, "sync")))
		emitters = append(emitters, signals.AionLiveSync(srv.AionLive()))
		// failed ritual runs page the feed (Phase 7; auto-clears on a newer
		// completed run of the same spirit/ritual)
		emitters = append(emitters, srv.RunFailureEmitter())
		// a down engine with queued work + a degraded deepseek endpoint (Phase 7)
		emitters = append(emitters, srv.EngineDownEmitter())
		// delegated work whose run completed while the todo is still open —
		// "your result is ready" (opens the report in place)
		emitters = append(emitters, srv.DelegationDoneEmitter())
		emitters = append(emitters, srv.PlanReadyEmitter()) // todo-panel Phase 4: materialize + page
		deepseekState := filepath.Join(cfg.DataDir, "portals", "deepseek.state.json")
		srv.UseDeepseekState(deepseekState)
		emitters = append(emitters, signals.DegradedPortal(deepseekState))
		if reSvc != nil {
			// property signals: over-budget category, stalled rehab, nothing-queued-next,
			// aging rock-tree tasks (overhaul decision 8)
			emitters = append(emitters, signals.OverBudgetProperties(reSvc),
				signals.StalledProperties(reSvc), signals.NoNextAction(reSvc),
				signals.StalePropertyTasks(reSvc))
		}
		srv.UseSignals(signals.New(sigStore, emitters...))
		log.Printf("feed signals: enabled (%d emitters)", len(emitters))
	}

	// PORTALS — external realms (ClickUp, Benchling) polled deterministically into
	// the FEED. Credentials live under DataDir/portals (0600), the poll cache under
	// DataDir/portal-cache — both outside the vault. Pollers run while the app is up.
	portalLoc := time.Local
	if cfg.Timezone != "" {
		if l, err := time.LoadLocation(cfg.Timezone); err == nil {
			portalLoc = l
		}
	}
	portalSvc := portals.New(cfg.DataDir, portalLoc)
	portalSvc.Start(ctx)
	srv.UsePortals(portalSvc)
	log.Printf("portals: enabled (clickup, benchling → FEED; creds under %s/portals)", cfg.DataDir)

	// SPIRITS — the excalibur harness console (plan §2.5: this replaced the
	// Hermes cockpit). The dashboard renders the sibling tree and records
	// user decisions; the engine (a separate process) owns all execution. The
	// approvals inbox is the excalibur surface (warden findings today, the
	// goals-Phase-2 EA later). Save-to-vault stays the one vault write.
	srv.UseVault(vw)
	if len(cfg.Harnesses) > 0 {
		// Harness federation (big-change Phase 4): one store pair per tree,
		// primary first. The primary keeps every write surface (spool, ritual
		// editor, aion sink); the rest surface read-side, tagged.
		var hs []server.Harness
		for i, ref := range cfg.Harnesses {
			sp := spiritsStore // the primary store already exists (aion sink holds it)
			if i > 0 || sp == nil {
				sp = spirits.NewStore(ref.Path).WithSkillsRoot(filepath.Join(cfg.VaultPath, "skills"))
			}
			ap := approvals.NewStore(filepath.Join(ref.Path, "artifacts")).
				WithVaultRoot(cfg.VaultPath).WithVaultWriter(vw).WithAionCapability("aion-approved").WithReCapability("realestate-approved")
			hs = append(hs, server.Harness{Name: ref.Name, Surface: ref.Surface, Spirits: sp, Approvals: ap})
		}
		srv.UseHarnesses(hs) // sets the primary spirits+approvals fields too
		// Hermes routes off the excalibur harness onto the owner's REAL do-bot
		// (the local Hermes Agent CLI) when enabled; plan/comment turns run with
		// a read-only toolset scope (the approval-gate pre-stage). Off → the
		// legacy harness path is unchanged.
		if cfg.Hermes.Enabled {
			srv.UseHermes(hermes.NewRunner(hermes.Config{
				Enabled: true, Bin: cfg.Hermes.Bin, Model: cfg.Hermes.Model,
				Toolsets: cfg.Hermes.Toolsets, TimeoutSeconds: cfg.Hermes.TimeoutSeconds,
			}), orDefault(cfg.Hermes.ReadToolsets, DefaultHermesReadToolsets))
		}
		srv.UseAionSink(sinkFan{aionSink, reSink}) // transcript-confirm → instant extraction spool (both domains)
		// email-sync auto-append (standing consent): a confirmed thread note
		// authorizes later appends, so matching append-vault-note proposals
		// apply without a card; refusals stay pending and render normally.
		for _, h := range hs {
			ap := h.Approvals
			go func() {
				time.Sleep(20 * time.Second)
				tick := time.NewTicker(60 * time.Second)
				defer tick.Stop()
				for {
					applied, _ := ap.AutoApplyAppends(func(paths []string) {
						sinkFan{aionSink, reSink}.Notify(paths)
					})
					if applied > 0 {
						log.Printf("approvals: auto-applied %d thread append(s)", applied)
					}
					select {
					case <-ctx.Done():
						return
					case <-tick.C:
					}
				}
			}()
		}
		names := make([]string, len(hs))
		for i, h := range hs {
			names[i] = h.Name
		}
		log.Printf("spirits: %d harness(es) [%s] (primary: %s)", len(hs), strings.Join(names, ", "), hs[0].Name)
	} else {
		log.Printf("spirits: disabled (set harnesses or excaliburPath in config to enable the SPIRITS tab)")
	}
	switch {
	case calClient.Enabled():
		log.Printf("google calendar: connected")
	case calClient.NeedsAuth():
		log.Printf("google calendar: credentials found but not authorized (connect from the dashboard)")
	default:
		log.Printf("google calendar: disabled (no credentials in ~/.config/manifest/)")
	}
	// AION PORTAL — a SECOND, separate listener (phase 1 of the portal move).
	// Its own mux, its own port, sharing nothing with the dashboard's routes:
	// the embedded web/portal subtree served as a standalone static site. A
	// bind failure here never takes the dashboard down.
	//
	// Phase 2–4 (2026-08-14): when aionPortal.teamDir is set, the listener
	// gains Google sign-in (@aion.bio wildcard) + the team write endpoints,
	// and team writes bridge into the FEED as notices. Credentials live in
	// the secrets tier (<dataDir>/portals/aion-portal-oauth.json, 0600).
	var portalOpts server.PortalOptions
	var aionTeamStore *teamportal.Store // shared with the todo-panel thread router
	if cfg.AionPortal.TeamDir != "" {
		if ts, err := teamportal.New(cfg.AionPortal.TeamDir); err != nil {
			log.Printf("aion portal team layer disabled: %v", err)
		} else {
			tokens := teamportal.NewTokens(cfg.DataDir)
			auth := teamportal.NewAuth(cfg.DataDir).WithTokens(tokens)
			portalOpts = server.PortalOptions{Auth: auth, Tokens: tokens, Store: ts, AdminEmail: cfg.AionPortal.AdminEmail, Live: srv.AionLive()}
			migrationActor := teamportal.Identity{Email: cfg.AionPortal.AdminEmail, Name: "Manifest migration"}
			if n, migrateErr := ts.MigrateItemIDs(aionStore.LegacyIDMap(), migrationActor, time.Now()); migrateErr != nil {
				log.Printf("aion collaboration id migration failed: %v", migrateErr)
			} else if n > 0 {
				log.Printf("aion: migrated collaboration for %d legacy item id(s)", n)
			}
			srv.UseTeamPortal(teamportal.NewBridge(ts, cfg.DataDir, cfg.AionPortal.AdminEmail), ts, cfg.AionPortal.AdminEmail)
			if orphans := srv.AionLive().OrphanTeamIDs(); len(orphans) > 0 {
				log.Printf("aion live sync: unresolved legacy collaboration ids (not guessed): %s", strings.Join(orphans, ", "))
			}
			aionTeamStore = ts
			if auth.Enabled() {
				log.Printf("aion portal team layer: enabled (writes → %s; any @%s account)", cfg.AionPortal.TeamDir, teamportal.Domain)
			} else {
				log.Printf("aion portal team layer: store ready, OAuth client missing (sign-in sealed until %s/portals/aion-portal-oauth.json exists)", cfg.DataDir)
			}
		}
	}
	if portalOpts.Live == nil {
		portalOpts.Live = srv.AionLive()
	}
	// TODO PANEL (todo-panel plan Phases 1-2): the plan-record layer over
	// system/todo-plans + the three-way thread stores. The private store is
	// per-machine dataDir; the shared RE store waits for a future RE surface;
	// aion todos comment through the portal's own team store (+ a blob-only
	// threads.Store rooted at the same shared dir for attachments).
	srv.UseTaskPlans(filepath.Join(cfg.SystemRoot, "todo-plans"))
	{
		private, err := threads.New(filepath.Join(cfg.DataDir, "todo-threads"))
		if err != nil {
			log.Printf("todo threads disabled: %v", err)
		} else {
			var reStore, aionBlobs *threads.Store
			if cfg.RealEstate.TeamDir != "" {
				if reStore, err = threads.New(cfg.RealEstate.TeamDir); err != nil {
					log.Printf("shared RE thread store disabled: %v", err)
				} else {
					log.Printf("re team threads: enabled (writes → %s)", cfg.RealEstate.TeamDir)
				}
			}
			if aionTeamStore != nil {
				if aionBlobs, err = threads.New(cfg.AionPortal.TeamDir); err != nil {
					log.Printf("aion thread blobs disabled: %v", err)
				}
			}
			srv.UseThreads(private, reStore, aionTeamStore, aionBlobs, cfg.AionPortal.AdminEmail)
			// native chat with kairos (chat-kairos handoff): shared threads on
			// the same portal volume, ingested by the AgentLoopTicker's chatSweep.
			if aionTeamStore != nil && cfg.AionPortal.TeamDir != "" {
				if chatStore, cerr := chatthreads.New(filepath.Join(cfg.AionPortal.TeamDir, "chat")); cerr != nil {
					log.Printf("portal chat disabled: %v", cerr)
				} else {
					srv.UseChatThreads(chatStore)
					log.Printf("portal chat: enabled (writes → %s/chat)", cfg.AionPortal.TeamDir)
				}
			}
			// the agent dialog must not wait for a feed read — ingestion
			// (plan attach/update, questions, relay retries) ticks on its own
			go srv.AgentLoopTicker()
		}
	}
	// LEDGER — the daily shared thread (persona plan Phase 0): a tier-3 JSONL
	// projection under dataDir, one file per owner-timezone day. Foreground
	// hooks append at write time; the AgentLoopTicker mirrors runs + chat.
	if led, err := ledger.New(filepath.Join(cfg.DataDir, "ledger"), portalLoc); err != nil {
		log.Printf("ledger disabled: %v", err)
	} else {
		srv.UseLedger(led)
		log.Printf("ledger: enabled (%s/ledger, day = %s)", cfg.DataDir, portalLoc)
	}
	// AGENT PERSONAS (persona plan Phase 1) — seed the three intent records
	// write-once; both the app (work-order preambles) and the agents (vault
	// spellbook) read them from the system zone.
	{
		personaRoot := filepath.ToSlash(filepath.Join(cfg.SystemRoot, "agents", "personas"))
		for intent, body := range server.SeedPersonas {
			if _, err := vw.CreateRecord(personaRoot+"/"+intent+".md", body); err != nil {
				log.Printf("persona seed %s: %v", intent, err)
			}
		}
		srv.UsePersonas(filepath.Join(cfg.VaultPath, filepath.FromSlash(personaRoot)), personaRoot)
	}
	// The portal↔cockpit agent bridges (kairos plan): wired HERE, after srv is
	// fully configured (threads, harnesses, personas all set above) and before
	// the portal listener starts.
	if aionTeamStore != nil {
		portalOpts.OnComment = srv.AionThreadHook
		portalOpts.Agents = srv.AionTeamAgents
		portalOpts.Panel = srv.AionPanel
		portalOpts.Assign = srv.AionAssign
		portalOpts.Fire = srv.AionFire
		portalOpts.Activity = srv.AionActivity
		portalOpts.PlanWrite = srv.AionPlanWrite
		portalOpts.FileBlob = srv.AionFileBlob
		// native chat with kairos (chat-kairos handoff)
		portalOpts.ChatThreads = srv.AionChatThreads
		portalOpts.ChatThread = srv.AionChatThread
		portalOpts.ChatAsk = srv.AionChatAsk
		portalOpts.ChatEngine = srv.AionChatEngine
		portalOpts.ChatProposal = srv.AionChatProposal
	}
	if cfg.PortalPort != 0 && cfg.PortalPort != cfg.Port {
		portalAddr := fmt.Sprintf("127.0.0.1:%d", cfg.PortalPort)
		if h, err := server.PortalHandler(portalOpts); err != nil {
			log.Printf("aion portal disabled: %v", err)
		} else {
			go func() {
				fmt.Printf("aion portal → http://%s\n", portalAddr)
				if err := http.ListenAndServe(portalAddr, h); err != nil {
					log.Printf("aion portal listener stopped: %v", err)
				}
			}()
		}
	}
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	fmt.Printf("manifest → http://%s  (vault: %s)\n", addr, cfg.VaultPath)
	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}

func orNone(s string) string {
	if s == "" {
		return "(none yet)"
	}
	return s
}

// calAdapter adapts the calendar client to the contacts CalendarReader interface
// (future, non-declined events with their non-self attendees).
type calAdapter struct{ c *calendar.Client }

func (a calAdapter) Upcoming(now time.Time, days int) []contacts.Event {
	if a.c == nil || !a.c.Enabled() {
		return nil
	}
	evs, err := a.c.Events(context.Background(), now, now.AddDate(0, 0, days))
	if err != nil {
		return nil
	}
	var out []contacts.Event
	for _, e := range evs {
		if e.Declined || e.Start.Before(now) {
			continue
		}
		ce := contacts.Event{Start: e.Start, Title: e.Title}
		for _, at := range e.Attendees {
			ce.Attendees = append(ce.Attendees, contacts.Attendee{Name: at.Name, Email: at.Email})
		}
		out = append(out, ce)
	}
	return out
}

// PastMeetings adapts the calendar client for calendar-verified "last met":
// timed (non-all-day), non-declined past events that have ≥1 non-self attendee.
func (a calAdapter) PastMeetings(now time.Time, days int) []contacts.Event {
	if a.c == nil || !a.c.Enabled() {
		return nil
	}
	evs, err := a.c.Events(context.Background(), now.AddDate(0, 0, -days), now)
	if err != nil {
		return nil
	}
	var out []contacts.Event
	for _, e := range evs {
		if e.Declined || e.AllDay || !e.Start.Before(now) {
			continue
		}
		ce := contacts.Event{Start: e.Start, Title: e.Title}
		for _, at := range e.Attendees {
			ce.Attendees = append(ce.Attendees, contacts.Attendee{Name: at.Name, Email: at.Email})
		}
		if len(ce.Attendees) == 0 {
			continue // a real meeting has at least one other person
		}
		out = append(out, ce)
	}
	return out
}

func vaultConfig(c Config) vault.Config {
	return vault.Config{
		Root:        c.VaultPath,
		NewDailyDir: c.NewDailyDir,
		GoalsName:   c.GoalsFileName,
		SkipDirs:    c.SkipDirs,
		SystemRoot:  c.SystemRoot, // zone short-circuit: no dailies/goals under system/
	}
}

func dailyConfig(c Config) daily.Config {
	return daily.Config{
		VaultPath:     c.VaultPath,
		ScheduleStart: c.ScheduleStart,
		ScheduleEnd:   c.ScheduleEnd,
	}
}

// orDefault returns s unless blank.
func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// sinkFan fans one transcript-confirm nudge out to every extraction sink —
// a freshly-confirmed note may carry any domain's category, and each sink
// filters for itself. Nil members are skipped (engine-less runs).
type sinkFan []interface{ Notify([]string) }

func (f sinkFan) Notify(paths []string) {
	for _, s := range f {
		if s != nil && !isNilSink(s) {
			s.Notify(paths)
		}
	}
}

// isNilSink guards the typed-nil-in-interface case (*aion.ExtractSink)(nil).
func isNilSink(s interface{ Notify([]string) }) bool {
	sink, ok := s.(*aion.ExtractSink)
	return ok && sink == nil
}
