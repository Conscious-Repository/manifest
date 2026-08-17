package fundraising

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

type SyncSheetRow struct {
	ID         string
	MetadataID int64
	Row        int // zero-based grid row
	Record     SharedOpportunity
	Sync       string
}

type SheetData struct {
	Initialized bool
	RowCount    int
	Rows        []SyncSheetRow
}

type SheetChange struct {
	Row              int
	MetadataID       int64
	ID               string
	Record           SharedOpportunity
	Sync             string
	AttachMetadata   bool
	Clear            bool
	DeleteMetadataID int64
}

type SheetInitResult struct {
	DryRun      bool   `json:"dryRun"`
	AlreadyDone bool   `json:"alreadyDone"`
	BackupTitle string `json:"backupTitle,omitempty"`
	Rows        int    `json:"rows"`
}

type SheetBackend interface {
	Read(context.Context) (SheetData, error)
	Write(context.Context, []SheetChange) error
	Initialize(context.Context, []SharedOpportunity, bool) (SheetInitResult, error)
}

type SyncConflict struct {
	ID       string `json:"id"`
	Firm     string `json:"firm"`
	Field    string `json:"field"`
	Manifest string `json:"manifest"`
	Sheet    string `json:"sheet"`
}

type SyncStatus struct {
	Enabled        bool           `json:"enabled"`
	Initialized    bool           `json:"initialized"`
	SpreadsheetURL string         `json:"spreadsheetUrl,omitempty"`
	LastAttempt    string         `json:"lastAttempt,omitempty"`
	LastSuccess    string         `json:"lastSuccess,omitempty"`
	LastError      string         `json:"lastError,omitempty"`
	Conflicts      []SyncConflict `json:"conflicts"`
}

type syncRecordState struct {
	Base      map[string]string       `json:"base"`
	Conflicts map[string]SyncConflict `json:"conflicts,omitempty"`
}

type syncDiskState struct {
	Version int                        `json:"version"`
	Records map[string]syncRecordState `json:"records"`
}

// SheetSync coordinates the one shared writer path between Manifest and a
// collaborator-facing Google Sheet.
type SheetSync struct {
	store          *Store
	backend        SheetBackend
	statePath      string
	auditPath      string
	spreadsheetURL string
	project        func() []Opportunity

	runMu sync.Mutex
	mu    sync.RWMutex
	state syncDiskState
	view  SyncStatus
}

func NewSheetSync(store *Store, backend SheetBackend, statePath, spreadsheetURL string, project func() []Opportunity) *SheetSync {
	s := &SheetSync{store: store, backend: backend, statePath: statePath, auditPath: filepath.Join(filepath.Dir(statePath), "sheet-activity.jsonl"), spreadsheetURL: spreadsheetURL, project: project}
	s.state = syncDiskState{Version: 1, Records: map[string]syncRecordState{}}
	s.view = SyncStatus{Enabled: backend != nil, SpreadsheetURL: spreadsheetURL, Conflicts: []SyncConflict{}}
	_ = s.load()
	s.refreshConflicts()
	return s
}

func (s *SheetSync) Start(ctx context.Context, interval time.Duration) {
	if s == nil || s.backend == nil || interval <= 0 {
		return
	}
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				_ = s.Sync(ctx)
				timer.Reset(interval)
			}
		}
	}()
}

func (s *SheetSync) Status() SyncStatus {
	if s == nil {
		return SyncStatus{Conflicts: []SyncConflict{}}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.view
	out.Conflicts = append([]SyncConflict(nil), s.view.Conflicts...)
	return out
}

func (s *SheetSync) Initialize(ctx context.Context, dryRun bool) (SheetInitResult, error) {
	if s == nil || s.backend == nil {
		return SheetInitResult{}, errors.New("fundraising sheet sync is disabled")
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	ops := s.snapshot()
	shared := make([]SharedOpportunity, 0, len(ops))
	for _, op := range ops {
		shared = append(shared, SharedFromOpportunity(op))
	}
	result, err := s.backend.Initialize(ctx, shared, dryRun)
	if err != nil || dryRun {
		return result, err
	}
	s.state = syncDiskState{Version: 1, Records: map[string]syncRecordState{}}
	for _, op := range shared {
		s.state.Records[op.ID] = syncRecordState{Base: sharedFieldMap(op), Conflicts: map[string]SyncConflict{}}
	}
	if err := s.save(); err != nil {
		return result, err
	}
	s.mu.Lock()
	s.view.Initialized = true
	s.view.LastSuccess = time.Now().UTC().Format(time.RFC3339)
	s.view.LastError = ""
	s.mu.Unlock()
	return result, nil
}

func (s *SheetSync) Sync(ctx context.Context) error {
	if s == nil || s.backend == nil {
		return errors.New("fundraising sheet sync is disabled")
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	s.view.LastAttempt = now
	s.mu.Unlock()
	data, err := s.backend.Read(ctx)
	if err != nil {
		s.fail(err)
		return err
	}
	if !data.Initialized {
		err := errors.New("fundraising sheet has not been initialized")
		s.fail(err)
		return err
	}
	originalState := cloneSyncState(s.state)

	rowsByID := map[string]SyncSheetRow{}
	clearedByID := map[string]SyncSheetRow{}
	occupied := map[int]bool{0: true}
	newRows := []SyncSheetRow{}
	for _, row := range data.Rows {
		occupied[row.Row] = true
		if row.ID == "" {
			newRows = append(newRows, row)
			continue
		}
		if strings.TrimSpace(row.Record.Firm) == "" {
			clearedByID[row.ID] = row
			continue
		}
		rowsByID[row.ID] = row
	}
	nextFree := func() int {
		for row := 1; ; row++ {
			if !occupied[row] {
				occupied[row] = true
				return row
			}
		}
	}

	writes := []SheetChange{}
	// New collaborator-authored rows are validated and created first.
	for _, row := range newRows {
		op, createErr := s.store.CreateShared(row.Record)
		if createErr != nil {
			writes = append(writes, SheetChange{Row: row.Row, Record: row.Record, Sync: "error: " + shortError(createErr)})
			continue
		}
		shared := SharedFromOpportunity(op)
		s.audit("create", op.ID, []string{"firm"})
		s.state.Records[op.ID] = syncRecordState{Base: sharedFieldMap(shared), Conflicts: map[string]SyncConflict{}}
		writes = append(writes, SheetChange{Row: row.Row, ID: op.ID, Record: shared, Sync: "synced", AttachMetadata: true})
		rowsByID[op.ID] = SyncSheetRow{ID: op.ID, Row: row.Row, Record: shared, Sync: "synced"}
	}

	ops := s.snapshot()
	current := map[string]Opportunity{}
	for _, op := range ops {
		current[op.ID] = op
	}
	ids := make([]string, 0, len(current))
	for id := range current {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		op := current[id]
		manifest := SharedFromOpportunity(op)
		row, exists := rowsByID[id]
		if !exists {
			if cleared, ok := clearedByID[id]; ok {
				row = cleared
			} else {
				row = SyncSheetRow{ID: id, Row: nextFree()}
			}
			s.state.Records[id] = syncRecordState{Base: sharedFieldMap(manifest), Conflicts: map[string]SyncConflict{}}
			writes = append(writes, SheetChange{Row: row.Row, MetadataID: row.MetadataID, ID: id, Record: manifest, Sync: "synced", AttachMetadata: row.MetadataID == 0})
			continue
		}
		st, tracked := s.state.Records[id]
		if st.Base == nil {
			st.Base = sharedFieldMap(manifest)
		}
		if st.Conflicts == nil {
			st.Conflicts = map[string]SyncConflict{}
		}
		manifestFields := sharedFieldMap(manifest)
		sheetFields := sharedFieldMap(row.Record)
		merged := cloneFields(manifestFields)
		pull := []string{}
		conflicts := map[string]SyncConflict{}
		for _, field := range sharedEditableFields {
			base := st.Base[field]
			mv, sv := manifestFields[field], sheetFields[field]
			if !tracked && mv != sv {
				conflicts[field] = SyncConflict{ID: id, Firm: manifest.Firm, Field: field, Manifest: mv, Sheet: sv}
				continue
			}
			switch {
			case sv == base:
				merged[field] = mv
				st.Base[field] = mv
			case mv == base:
				merged[field] = sv
				pull = append(pull, field)
				st.Base[field] = sv
			case mv == sv:
				merged[field] = mv
				st.Base[field] = mv
			default:
				conflicts[field] = SyncConflict{ID: id, Firm: manifest.Firm, Field: field, Manifest: mv, Sheet: sv}
			}
		}
		desired, validationErr := sharedWithFields(manifest, merged)
		if validationErr != nil {
			writes = append(writes, SheetChange{Row: row.Row, MetadataID: row.MetadataID, ID: id, Record: row.Record, Sync: "error: " + shortError(validationErr)})
			continue
		}
		if len(pull) > 0 {
			updated, updateErr := s.store.SharedUpdate(id, desired, pull)
			if updateErr != nil {
				writes = append(writes, SheetChange{Row: row.Row, MetadataID: row.MetadataID, ID: id, Record: row.Record, Sync: "error: " + shortError(updateErr)})
				continue
			}
			computed := manifest.ComputedLastTouchpoint
			manifest = SharedFromOpportunity(updated)
			manifest.ComputedLastTouchpoint = computed
			manifestFields = sharedFieldMap(manifest)
			s.audit("edit", id, pull)
		}
		st.Conflicts = conflicts
		s.state.Records[id] = st
		out := manifest
		outFields := sharedFieldMap(out)
		for field, conflict := range conflicts {
			outFields[field] = conflict.Sheet
		}
		out, _ = sharedWithFields(out, outFields)
		syncLabel := "synced"
		if len(conflicts) > 0 {
			syncLabel = fmt.Sprintf("conflict: %s", strings.Join(sortedKeys(conflicts), ", "))
		}
		if !sharedEqual(row.Record, out) || row.Sync != syncLabel {
			writes = append(writes, SheetChange{Row: row.Row, MetadataID: row.MetadataID, ID: id, Record: out, Sync: syncLabel})
		}
	}

	// A tracked row whose Markdown record is gone was deleted by the owner.
	for id, row := range rowsByID {
		if _, ok := current[id]; ok {
			continue
		}
		if _, tracked := s.state.Records[id]; tracked {
			writes = append(writes, SheetChange{Row: row.Row, MetadataID: row.MetadataID, ID: id, Clear: true, DeleteMetadataID: row.MetadataID})
			delete(s.state.Records, id)
		} else {
			writes = append(writes, SheetChange{Row: row.Row, MetadataID: row.MetadataID, ID: id, Record: row.Record, Sync: "error: unknown Manifest ID"})
		}
	}
	if err := s.backend.Write(ctx, writes); err != nil {
		s.state = originalState
		s.fail(err)
		return err
	}
	if err := s.save(); err != nil {
		s.state = originalState
		s.fail(err)
		return err
	}
	s.mu.Lock()
	s.view.Initialized = true
	s.view.LastSuccess = now
	s.view.LastError = ""
	s.mu.Unlock()
	s.refreshConflicts()
	return nil
}

func (s *SheetSync) Resolve(ctx context.Context, id, field, choice string) error {
	if choice != "manifest" && choice != "sheet" {
		return errors.New("choice must be manifest or sheet")
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	st, ok := s.state.Records[id]
	if !ok || st.Conflicts[field].Field == "" {
		return errors.New("sync conflict not found")
	}
	originalState := cloneSyncState(s.state)
	data, err := s.backend.Read(ctx)
	if err != nil {
		return err
	}
	var row *SyncSheetRow
	for i := range data.Rows {
		if data.Rows[i].ID == id {
			row = &data.Rows[i]
			break
		}
	}
	if row == nil {
		return errors.New("conflicted Sheet row not found")
	}
	op, ok := s.store.Get(id)
	if !ok {
		return errors.New("opportunity not found")
	}
	if choice == "sheet" {
		desired := SharedFromOpportunity(op)
		fields := sharedFieldMap(desired)
		fields[field] = st.Conflicts[field].Sheet
		desired, err = sharedWithFields(desired, fields)
		if err != nil {
			return err
		}
		op, err = s.store.SharedUpdate(id, desired, []string{field})
		if err != nil {
			return err
		}
	}
	shared := SharedFromOpportunity(op)
	st.Base[field] = sharedFieldMap(shared)[field]
	delete(st.Conflicts, field)
	s.state.Records[id] = st
	label := "synced"
	if len(st.Conflicts) > 0 {
		label = "conflict: " + strings.Join(sortedKeys(st.Conflicts), ", ")
	}
	if err := s.backend.Write(ctx, []SheetChange{{Row: row.Row, MetadataID: row.MetadataID, ID: id, Record: shared, Sync: label}}); err != nil {
		s.state = originalState
		return err
	}
	if err := s.save(); err != nil {
		s.state = originalState
		return err
	}
	s.refreshConflicts()
	return nil
}

func (s *SheetSync) snapshot() []Opportunity {
	if s.project != nil {
		return s.project()
	}
	ops, _ := s.store.List()
	return ops
}

func (s *SheetSync) load() error {
	b, err := os.ReadFile(s.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state syncDiskState
	if err := json.Unmarshal(b, &state); err != nil {
		return err
	}
	if state.Records == nil {
		state.Records = map[string]syncRecordState{}
	}
	s.state = state
	return nil
}

func (s *SheetSync) save() error {
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.statePath + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.statePath)
}

func (s *SheetSync) audit(action, id string, fields []string) {
	if err := os.MkdirAll(filepath.Dir(s.auditPath), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(s.auditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(map[string]any{
		"at": time.Now().UTC().Format(time.RFC3339), "actor": "google-sheet",
		"action": action, "id": id, "fields": fields,
	})
	_, _ = f.Write(append(b, '\n'))
}

func (s *SheetSync) fail(err error) {
	s.mu.Lock()
	s.view.LastError = shortError(err)
	s.mu.Unlock()
}

func (s *SheetSync) refreshConflicts() {
	conflicts := []SyncConflict{}
	for _, st := range s.state.Records {
		for _, c := range st.Conflicts {
			conflicts = append(conflicts, c)
		}
	}
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Firm != conflicts[j].Firm {
			return strings.ToLower(conflicts[i].Firm) < strings.ToLower(conflicts[j].Firm)
		}
		return conflicts[i].Field < conflicts[j].Field
	})
	s.mu.Lock()
	s.view.Conflicts = conflicts
	s.mu.Unlock()
}

func shortError(err error) string {
	if err == nil {
		return ""
	}
	v := strings.TrimSpace(err.Error())
	if len(v) > 120 {
		v = v[:120]
	}
	return v
}

func cloneFields(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneSyncState(in syncDiskState) syncDiskState {
	out := syncDiskState{Version: in.Version, Records: make(map[string]syncRecordState, len(in.Records))}
	for id, record := range in.Records {
		copyRecord := syncRecordState{Base: cloneFields(record.Base), Conflicts: map[string]SyncConflict{}}
		for field, conflict := range record.Conflicts {
			copyRecord.Conflicts[field] = conflict
		}
		out.Records[id] = copyRecord
	}
	return out
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sharedEqual(a, b SharedOpportunity) bool {
	return reflect.DeepEqual(sharedFieldMap(a), sharedFieldMap(b)) &&
		a.ComputedLastTouchpoint == b.ComputedLastTouchpoint && a.Archived == b.Archived
}
