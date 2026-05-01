# Authenticated scraping with session reuse

Use case: log in to a site once (with MFA), save the session, then have an agent scrape data behind the login on subsequent runs.

## Step 1: Manual login + save_session

Open Chrome, log in to the target site normally (entering MFA codes if required).

In your MCP client (Claude Desktop, Trinetra, etc.), invoke:

```jsonc
// 1. Attach to the running Chrome
meta_attach: {}

// 2. Save the current cookies under a name
task_save_session: {"name": "target-site"}
```

Output: `{"ok": true, "session_path": "/home/you/.config/netra-browser/sessions/target-site.json"}`

## Step 2: Headless replay later

In a new session (possibly from a fresh Chrome instance):

```jsonc
meta_attach: {"debug_url": "http://127.0.0.1:9222"}
task_load_session: {"name": "target-site"}

browser_new_tab: {"url": "https://target-site/dashboard"}
browser_navigate: {"url": "https://target-site/dashboard", "wait_until": "load"}

// You're now on an authenticated page.
browser_snapshot: {"mode": "accessibility"}
// Use the snapshot ids to extract data:
browser_eval: {"expression": "document.querySelector('.account-balance').textContent"}
```

## Notes

- v1 saves cookies only. localStorage / sessionStorage are not yet preserved.
- The session file is portable across Chrome instances on the same machine. Cross-machine use works if the target site doesn't bind cookies to a device fingerprint.
