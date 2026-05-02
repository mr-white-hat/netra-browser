# Bug bounty workflow

Bug bounty agents need to drive *real* targets — production sites with MFA, anti-bot, browser fingerprinting. `netra-browser` is built for this.

## Pattern: signup loop with disposable accounts

```jsonc
// 1. Set up: launch Chrome with your Burp proxy
meta_attach: {}

// 2. For each iteration:
browser_new_tab: {"url": "https://target/signup"}
browser_navigate: {"url": "https://target/signup", "wait_until": "load"}
browser_fill: {"locator": {"css": "#email"}, "value": "user+0001@example.com"}
browser_fill: {"locator": {"css": "#password"}, "value": "AutoGen-Pw-0001"}
browser_click: {"locator": {"role": "button", "name": "Sign up"}}

// 3. Wait for confirmation email/page
browser_wait_for: {"event": "navigation", "timeout_ms": 10000}

// 4. Capture HAR for analysis
task_capture_har: {"duration_ms": 5000}
```

## Pattern: capture JWT after auth

```jsonc
browser_navigate: {"url": "https://target/login", "wait_until": "load"}
// (manual login if needed)
browser_get_recent_events: {"types": ["network_request"]}
// Filter for the request that contains the JWT in Authorization headers.
```

## Integration with an external bug-bounty agent

Run `netra-browser --listen 127.0.0.1:7878 --token $TOKEN` and have your agent POST to `/rpc`. The bridge stays neutral — domain-specific workflows (recon orchestration, scope checks, loot management, vulnerability scanners) live in your agent; this is just the browser layer they consume.
