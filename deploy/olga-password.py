#!/usr/bin/env python3
"""Set Olga's sign-in password on metis without storing it in shell history."""
import getpass
import subprocess

password = getpass.getpass("Olga's Manifest password: ")
if not password or password != password.strip():
    raise SystemExit("Use a nonempty password without leading or trailing whitespace.")
if password != getpass.getpass("Repeat password: "):
    raise SystemExit("Passwords did not match; nothing changed.")
subprocess.run(
    ["ssh", "benjamin@metis.tail8f89de.ts.net",
     "umask 077; cat > /private/olga/password.new && mv /private/olga/password.new /private/olga/password"],
    input=(password + "\n").encode(), check=True,
)
print("Password set. Existing sessions are signed out; no restart needed.")
