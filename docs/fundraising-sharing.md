# Shared fundraising pipeline

Manifest remains the canonical store for fundraising opportunities. Google
Sheets is a restricted editing surface over the same records and does not
expose contact emails, note paths, vault paths, or team-portal state.

## Google Sheet sync

1. Create a dedicated Google Cloud service account with the Google Sheets API
   enabled. Download its JSON key to:

   ```text
   <dataDir>/fundraising/google-service-account.json
   ```

   The file must be owned by the Manifest service user and mode `0600`.

2. Share only the target workbook with the service account's `client_email` as
   an editor.

3. Add the configuration with synchronization initially disabled:

   ```json
   "fundraisingSheets": {
     "enabled": false,
     "spreadsheetId": "1jAxUzcJa3O_imx26uAiHAxxic4EJRYJ9aAJ2UCxOGRQ",
     "sheetId": 0,
     "credentialsPath": "<dataDir>/fundraising/google-service-account.json",
     "syncIntervalMinutes": 5
   }
   ```

4. Run the recoverable initializer preview on the server:

   ```bash
   go run ./cmd/fundraising-sheet-init \
     -vault <vault> -data-dir <dataDir> \
     -spreadsheet 1jAxUzcJa3O_imx26uAiHAxxic4EJRYJ9aAJ2UCxOGRQ \
     -sheet-id 0 -credentials <dataDir>/fundraising/google-service-account.json \
     -dry-run
   ```

5. Repeat without `-dry-run`. The command first duplicates the existing tab to
   a protected `Legacy import YYYY-MM-DD` backup, then upgrades the original
   `gid=0` tab and seeds the three-way sync state.

6. Set `enabled` to `true`, restart Manifest, press **Sync now** in the private
   fundraising view, and verify a round trip before sharing the workbook.

Sheet edits are attributed in Google version history. Manifest records them as
the generic `google-sheet` actor. Invalid cells remain in Sheets with an error;
conflicting fields require an owner choice in Manifest.
