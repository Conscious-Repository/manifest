# Shared fundraising pipeline

Manifest remains the canonical store for fundraising opportunities. Google
Sheets and `fundraising.aion.bio` are restricted editing surfaces over the same
records; neither exposes contact emails, note paths, vault paths, or team-portal
state.

## Google Sheet sync

1. Create a dedicated Google Cloud service account with the Google Sheets API
   enabled. Download its JSON key to:

   ```text
   <dataDir>/fundraising/google-service-account.json
   ```

   The file must be owned by the Manifest service user and mode `0600`.

2. Share only the target workbook with the service account's `client_email` as
   an editor.

3. Add the dark configuration:

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

## Branded external editor

1. Create a separate Google OAuth web client with this exact redirect URI:

   ```text
   https://fundraising.aion.bio/oauth2/callback
   ```

2. Install the client JSON at
   `<dataDir>/portals/fundraising-portal-oauth.json`, mode `0600`.

3. Configure a dedicated listener, separate from both the owner cockpit and
   `portal.aion.bio`:

   ```json
   "fundraisingPortal": {
     "port": 7779,
     "adminEmail": "owner@aion.bio",
     "oauthClient": "<dataDir>/portals/fundraising-portal-oauth.json"
   }
   ```

4. Route `fundraising.aion.bio` through the current HTTPS ingress to
   `127.0.0.1:7779`.

5. Add or revoke Google-account emails from the private fundraising **SYNC**
   panel. Revocation is checked on every request and immediately invalidates an
   existing session.

Invitees can view the complete shared pipeline and create or edit records. They
cannot archive, restore, delete, browse contacts, access Markdown, or enter the
team portal. Authenticated changes are appended to
`<dataDir>/fundraising/external-activity.jsonl`.
