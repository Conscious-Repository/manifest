package fundraising

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

const (
	sheetMetadataKey  = "manifestOpportunityID"
	sheetSchemaKey    = "manifestFundraisingSchema"
	sheetSchemaValue  = "1"
	sheetColumnCount  = 16
	defaultSheetTitle = "Fundraising"
)

var sheetHeaders = []string{
	"Firm", "Website", "People", "Source", "Status", "Interest", "Amount", "Currency",
	"Last Touchpoint", "Last Touchpoint Date", "Computed Last Touchpoint", "Next Step",
	"Next Step Due", "Notes", "Archived", "Sync",
}

type GoogleSheetConfig struct {
	SpreadsheetID   string
	SheetID         int64
	CredentialsPath string
}

type GoogleSheetBackend struct {
	service       *sheets.Service
	spreadsheetID string
	sheetID       int64
	clientEmail   string
}

func NewGoogleSheetBackend(ctx context.Context, cfg GoogleSheetConfig) (*GoogleSheetBackend, error) {
	if strings.TrimSpace(cfg.SpreadsheetID) == "" {
		return nil, errors.New("fundraising Sheets spreadsheet ID is required")
	}
	info, err := os.Stat(cfg.CredentialsPath)
	if err != nil {
		return nil, fmt.Errorf("fundraising Sheets credentials: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("fundraising Sheets credentials must be mode 0600")
	}
	b, err := os.ReadFile(cfg.CredentialsPath)
	if err != nil {
		return nil, err
	}
	var identity struct {
		ClientEmail string `json:"client_email"`
	}
	if err := json.Unmarshal(b, &identity); err != nil || identity.ClientEmail == "" {
		return nil, errors.New("fundraising Sheets credentials are not a service-account JSON")
	}
	service, err := sheets.NewService(ctx,
		option.WithCredentialsFile(cfg.CredentialsPath),
		option.WithScopes(sheets.SpreadsheetsScope),
	)
	if err != nil {
		return nil, err
	}
	return &GoogleSheetBackend{service: service, spreadsheetID: cfg.SpreadsheetID, sheetID: cfg.SheetID, clientEmail: identity.ClientEmail}, nil
}

func (g *GoogleSheetBackend) Read(ctx context.Context) (SheetData, error) {
	var spreadsheet *sheets.Spreadsheet
	err := googleRetry(ctx, func() error {
		var err error
		spreadsheet, err = g.service.Spreadsheets.Get(g.spreadsheetID).
			Fields("sheets(properties(sheetId,title,gridProperties(rowCount,columnCount)))").Context(ctx).Do()
		return err
	})
	if err != nil {
		return SheetData{}, err
	}
	props, err := g.sheetProperties(spreadsheet)
	if err != nil {
		return SheetData{}, err
	}
	initialized, err := g.initialized(ctx)
	if err != nil {
		return SheetData{}, err
	}
	if !initialized {
		return SheetData{Initialized: false, RowCount: int(props.GridProperties.RowCount)}, nil
	}
	rowCount := props.GridProperties.RowCount
	var values *sheets.ValueRange
	err = googleRetry(ctx, func() error {
		var err error
		values, err = g.service.Spreadsheets.Values.Get(g.spreadsheetID, quoteSheet(props.Title)+"!A2:P"+strconv.FormatInt(rowCount, 10)).
			ValueRenderOption("UNFORMATTED_VALUE").DateTimeRenderOption("FORMATTED_STRING").Context(ctx).Do()
		return err
	})
	if err != nil {
		return SheetData{}, err
	}
	metadata, err := g.rowMetadata(ctx)
	if err != nil {
		return SheetData{}, err
	}
	rows := []SyncSheetRow{}
	for i, raw := range values.Values {
		gridRow := i + 1
		id := metadata[gridRow]
		record := sharedFromCells(raw)
		syncValue := cellString(raw, 15)
		if id.Value == "" && !cellsHaveContent(raw) && syncValue == "" {
			continue
		}
		rows = append(rows, SyncSheetRow{ID: id.Value, MetadataID: id.MetadataID, Row: gridRow, Record: record, Sync: syncValue})
	}
	return SheetData{Initialized: true, RowCount: int(rowCount), Rows: rows}, nil
}

type rowMetadata struct {
	Value      string
	MetadataID int64
}

func (g *GoogleSheetBackend) rowMetadata(ctx context.Context) (map[int]rowMetadata, error) {
	request := &sheets.SearchDeveloperMetadataRequest{DataFilters: []*sheets.DataFilter{{DeveloperMetadataLookup: &sheets.DeveloperMetadataLookup{MetadataKey: sheetMetadataKey, Visibility: "DOCUMENT"}}}}
	var response *sheets.SearchDeveloperMetadataResponse
	err := googleRetry(ctx, func() error {
		var err error
		response, err = g.service.Spreadsheets.DeveloperMetadata.Search(g.spreadsheetID, request).Context(ctx).Do()
		return err
	})
	if err != nil {
		return nil, err
	}
	out := map[int]rowMetadata{}
	for _, match := range response.MatchedDeveloperMetadata {
		m := match.DeveloperMetadata
		if m == nil || m.Location == nil || m.Location.DimensionRange == nil || m.Location.DimensionRange.SheetId != g.sheetID {
			continue
		}
		r := int(m.Location.DimensionRange.StartIndex)
		out[r] = rowMetadata{Value: m.MetadataValue, MetadataID: m.MetadataId}
	}
	return out, nil
}

func (g *GoogleSheetBackend) initialized(ctx context.Context) (bool, error) {
	request := &sheets.SearchDeveloperMetadataRequest{DataFilters: []*sheets.DataFilter{{DeveloperMetadataLookup: &sheets.DeveloperMetadataLookup{MetadataKey: sheetSchemaKey, MetadataValue: sheetSchemaValue, Visibility: "DOCUMENT"}}}}
	var response *sheets.SearchDeveloperMetadataResponse
	err := googleRetry(ctx, func() error {
		var err error
		response, err = g.service.Spreadsheets.DeveloperMetadata.Search(g.spreadsheetID, request).Context(ctx).Do()
		return err
	})
	return err == nil && len(response.MatchedDeveloperMetadata) > 0, err
}

func (g *GoogleSheetBackend) Write(ctx context.Context, changes []SheetChange) error {
	if len(changes) == 0 {
		return nil
	}
	requests := make([]*sheets.Request, 0, len(changes)*2)
	for _, change := range changes {
		if change.Clear {
			requests = append(requests, &sheets.Request{UpdateCells: &sheets.UpdateCellsRequest{
				Range: gridRange(g.sheetID, int64(change.Row), int64(change.Row+1), 0, sheetColumnCount),
				Rows:  []*sheets.RowData{{Values: blankCells(sheetColumnCount)}}, Fields: "userEnteredValue",
			}})
			if change.DeleteMetadataID != 0 {
				requests = append(requests, &sheets.Request{DeleteDeveloperMetadata: &sheets.DeleteDeveloperMetadataRequest{DataFilter: &sheets.DataFilter{DeveloperMetadataLookup: &sheets.DeveloperMetadataLookup{MetadataId: change.DeleteMetadataID}}}})
			}
			continue
		}
		requests = append(requests, &sheets.Request{UpdateCells: &sheets.UpdateCellsRequest{
			Range: gridRange(g.sheetID, int64(change.Row), int64(change.Row+1), 0, sheetColumnCount),
			Rows:  []*sheets.RowData{{Values: sharedCells(change.Record, change.Sync)}}, Fields: "userEnteredValue,userEnteredFormat.numberFormat",
		}})
		if change.AttachMetadata {
			requests = append(requests, metadataCreate(g.sheetID, change.Row, change.ID))
		}
	}
	return g.batch(ctx, requests)
}

func (g *GoogleSheetBackend) Initialize(ctx context.Context, records []SharedOpportunity, dryRun bool) (SheetInitResult, error) {
	var spreadsheet *sheets.Spreadsheet
	err := googleRetry(ctx, func() error {
		var err error
		spreadsheet, err = g.service.Spreadsheets.Get(g.spreadsheetID).Fields("sheets(properties(sheetId,title,gridProperties(rowCount,columnCount)))").Context(ctx).Do()
		return err
	})
	if err != nil {
		return SheetInitResult{}, err
	}
	props, err := g.sheetProperties(spreadsheet)
	if err != nil {
		return SheetInitResult{}, err
	}
	if done, err := g.initialized(ctx); err != nil {
		return SheetInitResult{}, err
	} else if done {
		return SheetInitResult{DryRun: dryRun, AlreadyDone: true, Rows: len(records)}, nil
	}
	backupTitle := uniqueBackupTitle(spreadsheet, time.Now())
	result := SheetInitResult{DryRun: dryRun, BackupTitle: backupTitle, Rows: len(records)}
	if dryRun {
		return result, nil
	}

	// Copy first. If any later formatting request fails, the untouched source is
	// already recoverable in the same workbook.
	first := &sheets.BatchUpdateSpreadsheetRequest{Requests: []*sheets.Request{
		{DuplicateSheet: &sheets.DuplicateSheetRequest{SourceSheetId: g.sheetID, NewSheetName: backupTitle, ForceSendFields: []string{"SourceSheetId"}}},
		{UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{Properties: &sheets.SheetProperties{SheetId: g.sheetID, Title: defaultSheetTitle, ForceSendFields: []string{"SheetId"}}, Fields: "title"}},
	}}
	var firstResponse *sheets.BatchUpdateSpreadsheetResponse
	err = googleRetry(ctx, func() error {
		var err error
		firstResponse, err = g.service.Spreadsheets.BatchUpdate(g.spreadsheetID, first).Context(ctx).Do()
		return err
	})
	if err != nil {
		return result, err
	}
	if len(firstResponse.Replies) == 0 || firstResponse.Replies[0].DuplicateSheet == nil || firstResponse.Replies[0].DuplicateSheet.Properties == nil {
		return result, errors.New("Google Sheets did not return the backup sheet ID")
	}
	backupID := firstResponse.Replies[0].DuplicateSheet.Properties.SheetId
	rowCount := int(props.GridProperties.RowCount)
	if rowCount < len(records)+20 {
		rowCount = len(records) + 20
	}
	requests := []*sheets.Request{
		{RepeatCell: &sheets.RepeatCellRequest{Range: gridRange(g.sheetID, 0, props.GridProperties.RowCount, 0, props.GridProperties.ColumnCount), Cell: &sheets.CellData{}, Fields: "userEnteredValue,userEnteredFormat,dataValidation,note"}},
		{UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{Properties: &sheets.SheetProperties{SheetId: g.sheetID, GridProperties: &sheets.GridProperties{FrozenRowCount: 1}, ForceSendFields: []string{"SheetId"}}, Fields: "gridProperties.frozenRowCount"}},
		{AddProtectedRange: &sheets.AddProtectedRangeRequest{ProtectedRange: protected(gridRange(backupID, 0, 0, 0, 0), "Manifest legacy import backup", g.clientEmail)}},
	}
	header := make([]*sheets.CellData, 0, len(sheetHeaders))
	for _, name := range sheetHeaders {
		header = append(header, &sheets.CellData{UserEnteredValue: stringValue(name), UserEnteredFormat: &sheets.CellFormat{BackgroundColorStyle: rgb(0.92, 0.92, 0.92), TextFormat: &sheets.TextFormat{Bold: true}}})
	}
	rows := []*sheets.RowData{{Values: header}}
	for _, record := range records {
		rows = append(rows, &sheets.RowData{Values: sharedCells(record, "synced")})
	}
	requests = append(requests, &sheets.Request{UpdateCells: &sheets.UpdateCellsRequest{
		Range: gridRange(g.sheetID, 0, int64(len(rows)), 0, sheetColumnCount),
		Rows:  rows, Fields: "userEnteredValue,userEnteredFormat",
	}})
	for _, request := range validationRequests(g.sheetID, rowCount) {
		requests = append(requests, request)
	}
	requests = append(requests,
		&sheets.Request{SetBasicFilter: &sheets.SetBasicFilterRequest{Filter: &sheets.BasicFilter{Range: gridRange(g.sheetID, 0, int64(rowCount), 0, sheetColumnCount)}}},
		&sheets.Request{RepeatCell: &sheets.RepeatCellRequest{Range: gridRange(g.sheetID, 1, int64(rowCount), 2, 4), Cell: &sheets.CellData{UserEnteredFormat: &sheets.CellFormat{WrapStrategy: "WRAP"}}, Fields: "userEnteredFormat.wrapStrategy"}},
		&sheets.Request{RepeatCell: &sheets.RepeatCellRequest{Range: gridRange(g.sheetID, 1, int64(rowCount), 13, 14), Cell: &sheets.CellData{UserEnteredFormat: &sheets.CellFormat{WrapStrategy: "WRAP"}}, Fields: "userEnteredFormat.wrapStrategy"}},
		&sheets.Request{AddProtectedRange: &sheets.AddProtectedRangeRequest{ProtectedRange: protected(gridRange(g.sheetID, 1, int64(rowCount), 10, 11), "Computed by Manifest", g.clientEmail)}},
		&sheets.Request{AddProtectedRange: &sheets.AddProtectedRangeRequest{ProtectedRange: protected(gridRange(g.sheetID, 1, int64(rowCount), 14, 16), "Owner-controlled state", g.clientEmail)}},
		&sheets.Request{AutoResizeDimensions: &sheets.AutoResizeDimensionsRequest{Dimensions: dimensionRange(g.sheetID, "COLUMNS", 0, sheetColumnCount)}},
		&sheets.Request{UpdateDimensionProperties: &sheets.UpdateDimensionPropertiesRequest{Range: dimensionRange(g.sheetID, "COLUMNS", 13, 14), Properties: &sheets.DimensionProperties{PixelSize: 320}, Fields: "pixelSize"}},
	)
	for row, record := range records {
		requests = append(requests, metadataCreate(g.sheetID, row+1, record.ID))
	}
	requests = append(requests, &sheets.Request{CreateDeveloperMetadata: &sheets.CreateDeveloperMetadataRequest{DeveloperMetadata: &sheets.DeveloperMetadata{
		MetadataKey: sheetSchemaKey, MetadataValue: sheetSchemaValue, Visibility: "DOCUMENT",
		Location: &sheets.DeveloperMetadataLocation{Spreadsheet: true, ForceSendFields: []string{"Spreadsheet"}},
	}}})
	if err := g.batch(ctx, requests); err != nil {
		return result, err
	}
	return result, nil
}

func (g *GoogleSheetBackend) batch(ctx context.Context, requests []*sheets.Request) error {
	if len(requests) == 0 {
		return nil
	}
	return googleRetry(ctx, func() error {
		_, err := g.service.Spreadsheets.BatchUpdate(g.spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{Requests: requests}).Context(ctx).Do()
		return err
	})
}

func (g *GoogleSheetBackend) sheetProperties(spreadsheet *sheets.Spreadsheet) (*sheets.SheetProperties, error) {
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties != nil && sheet.Properties.SheetId == g.sheetID {
			return sheet.Properties, nil
		}
	}
	return nil, fmt.Errorf("Google Sheet gid %d not found", g.sheetID)
}

func sharedFromCells(row []any) SharedOpportunity {
	return SharedOpportunity{
		Firm: cellString(row, 0), Website: cellString(row, 1), People: splitPeople(cellString(row, 2)), Source: cellString(row, 3),
		Status: cellString(row, 4), Interest: cellString(row, 5), Amount: cellFloat(row, 6), Currency: cellString(row, 7),
		LastTouchpoint: cellString(row, 8), LastTouchpointDate: cellDate(row, 9), ComputedLastTouchpoint: cellDate(row, 10),
		NextStep: cellString(row, 11), NextStepDue: cellDate(row, 12), Notes: cellString(row, 13), Archived: cellBool(row, 14),
	}
}

func sharedCells(record SharedOpportunity, syncValue string) []*sheets.CellData {
	return []*sheets.CellData{
		textCell(record.Firm), textCell(record.Website), textCell(strings.Join(record.People, "; ")), textCell(record.Source),
		textCell(record.Status), textCell(record.Interest), numberCell(record.Amount, "#,##0.00"), textCell(record.Currency),
		textCell(record.LastTouchpoint), dateCell(record.LastTouchpointDate), dateCell(record.ComputedLastTouchpoint), textCell(record.NextStep),
		dateCell(record.NextStepDue), textCell(record.Notes), boolCell(record.Archived), textCell(syncValue),
	}
}

func validationRequests(sheetID int64, rowCount int) []*sheets.Request {
	rangeFor := func(col int64) *sheets.GridRange {
		return gridRange(sheetID, 1, int64(rowCount), col, col+1)
	}
	list := func(values ...string) *sheets.DataValidationRule {
		xs := make([]*sheets.ConditionValue, 0, len(values))
		for _, value := range values {
			xs = append(xs, &sheets.ConditionValue{UserEnteredValue: value})
		}
		return &sheets.DataValidationRule{Condition: &sheets.BooleanCondition{Type: "ONE_OF_LIST", Values: xs}, Strict: true, ShowCustomUi: true}
	}
	date := &sheets.DataValidationRule{Condition: &sheets.BooleanCondition{Type: "DATE_IS_VALID"}, Strict: false, ShowCustomUi: true}
	return []*sheets.Request{
		{SetDataValidation: &sheets.SetDataValidationRequest{Range: rangeFor(1), Rule: &sheets.DataValidationRule{Condition: &sheets.BooleanCondition{Type: "TEXT_IS_URL"}, Strict: false, InputMessage: "Use an http or https website URL."}}},
		{SetDataValidation: &sheets.SetDataValidationRequest{Range: rangeFor(4), Rule: list(Statuses...)}},
		{SetDataValidation: &sheets.SetDataValidationRequest{Range: rangeFor(5), Rule: list(Interests...)}},
		{SetDataValidation: &sheets.SetDataValidationRequest{Range: rangeFor(6), Rule: &sheets.DataValidationRule{Condition: &sheets.BooleanCondition{Type: "NUMBER_GREATER_THAN_EQ", Values: []*sheets.ConditionValue{{UserEnteredValue: "0"}}}, Strict: true}}},
		{SetDataValidation: &sheets.SetDataValidationRequest{Range: rangeFor(7), Rule: &sheets.DataValidationRule{Condition: &sheets.BooleanCondition{Type: "CUSTOM_FORMULA", Values: []*sheets.ConditionValue{{UserEnteredValue: "=OR(H2=\"\",REGEXMATCH(H2,\"^[A-Z]{3}$\"))"}}}, Strict: true, InputMessage: "Use a three-letter currency code such as USD."}}},
		{SetDataValidation: &sheets.SetDataValidationRequest{Range: rangeFor(9), Rule: date}},
		{SetDataValidation: &sheets.SetDataValidationRequest{Range: rangeFor(10), Rule: date}},
		{SetDataValidation: &sheets.SetDataValidationRequest{Range: rangeFor(12), Rule: date}},
	}
}

func metadataCreate(sheetID int64, row int, id string) *sheets.Request {
	return &sheets.Request{CreateDeveloperMetadata: &sheets.CreateDeveloperMetadataRequest{DeveloperMetadata: &sheets.DeveloperMetadata{
		MetadataKey: sheetMetadataKey, MetadataValue: id, Visibility: "DOCUMENT",
		Location: &sheets.DeveloperMetadataLocation{DimensionRange: dimensionRange(sheetID, "ROWS", int64(row), int64(row+1))},
	}}}
}

func gridRange(sheetID, startRow, endRow, startColumn, endColumn int64) *sheets.GridRange {
	return &sheets.GridRange{
		SheetId: sheetID, StartRowIndex: startRow, EndRowIndex: endRow,
		StartColumnIndex: startColumn, EndColumnIndex: endColumn,
		ForceSendFields: []string{"SheetId"},
	}
}

func dimensionRange(sheetID int64, dimension string, start, end int64) *sheets.DimensionRange {
	return &sheets.DimensionRange{SheetId: sheetID, Dimension: dimension, StartIndex: start, EndIndex: end, ForceSendFields: []string{"SheetId"}}
}

func protected(grid *sheets.GridRange, description, editor string) *sheets.ProtectedRange {
	return &sheets.ProtectedRange{Range: grid, Description: description, Editors: &sheets.Editors{Users: []string{editor}, ForceSendFields: []string{"DomainUsersCanEdit"}}}
}

func uniqueBackupTitle(spreadsheet *sheets.Spreadsheet, now time.Time) string {
	base := "Legacy import " + now.Format("2006-01-02")
	used := map[string]bool{}
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties != nil {
			used[sheet.Properties.Title] = true
		}
	}
	if !used[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)", base, n)
		if !used[candidate] {
			return candidate
		}
	}
}

func googleRetry(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		var apiErr *googleapi.Error
		if !errors.As(err, &apiErr) || (apiErr.Code != 429 && apiErr.Code < 500) {
			return err
		}
		delay := time.Duration(1<<attempt) * time.Second
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return err
}

func quoteSheet(title string) string { return "'" + strings.ReplaceAll(title, "'", "''") + "'" }
func cellString(row []any, col int) string {
	if col >= len(row) || row[col] == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(row[col]))
}
func cellsHaveContent(row []any) bool {
	for i, value := range row {
		if i == 15 || value == nil {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(value)) != "" {
			return true
		}
	}
	return false
}
func cellFloat(row []any, col int) float64 {
	if col >= len(row) || row[col] == nil {
		return 0
	}
	switch v := row[col].(type) {
	case float64:
		return v
	default:
		f, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(v)), 64)
		return f
	}
}
func cellBool(row []any, col int) bool {
	if col >= len(row) || row[col] == nil {
		return false
	}
	switch v := row[col].(type) {
	case bool:
		return v
	default:
		b, _ := strconv.ParseBool(strings.TrimSpace(fmt.Sprint(v)))
		return b
	}
}
func cellDate(row []any, col int) string {
	if col >= len(row) || row[col] == nil {
		return ""
	}
	if serial, ok := row[col].(float64); ok && serial > 0 {
		return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).Add(time.Duration(math.Round(serial*24)) * time.Hour).Format("2006-01-02")
	}
	return strings.TrimSpace(fmt.Sprint(row[col]))
}

func stringValue(value string) *sheets.ExtendedValue {
	return &sheets.ExtendedValue{StringValue: googleapi.String(value)}
}
func textCell(value string) *sheets.CellData {
	return &sheets.CellData{UserEnteredValue: stringValue(value)}
}
func numberCell(value float64, pattern string) *sheets.CellData {
	return &sheets.CellData{UserEnteredValue: &sheets.ExtendedValue{NumberValue: googleapi.Float64(value)}, UserEnteredFormat: &sheets.CellFormat{NumberFormat: &sheets.NumberFormat{Type: "NUMBER", Pattern: pattern}}}
}
func boolCell(value bool) *sheets.CellData {
	return &sheets.CellData{UserEnteredValue: &sheets.ExtendedValue{BoolValue: googleapi.Bool(value)}}
}
func dateCell(value string) *sheets.CellData {
	if value == "" {
		return textCell("")
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return textCell(value)
	}
	serial := parsed.Sub(time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)).Hours() / 24
	return &sheets.CellData{UserEnteredValue: &sheets.ExtendedValue{NumberValue: googleapi.Float64(serial)}, UserEnteredFormat: &sheets.CellFormat{NumberFormat: &sheets.NumberFormat{Type: "DATE", Pattern: "yyyy-mm-dd"}}}
}
func blankCells(n int) []*sheets.CellData {
	out := make([]*sheets.CellData, n)
	for i := range out {
		out[i] = textCell("")
	}
	return out
}
func rgb(r, g, b float64) *sheets.ColorStyle {
	return &sheets.ColorStyle{RgbColor: &sheets.Color{Red: r, Green: g, Blue: b}}
}
