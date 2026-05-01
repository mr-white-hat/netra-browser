# End-to-end QA flow

Use `netra-browser` from your CI to test that real-user flows work in the actual product.

## Sample: login + checkout

```jsonc
meta_attach: {}

// Load test user state
task_load_session: {"name": "qa-user"}

browser_new_tab: {"url": "https://shop.example/cart"}
browser_navigate: {"url": "https://shop.example/cart", "wait_until": "networkidle"}

browser_click: {"locator": {"role": "button", "name": "Checkout"}}
browser_fill: {"locator": {"role": "textbox", "name": "Card number"}, "value": "4242 4242 4242 4242"}
browser_click: {"locator": {"role": "button", "name": "Pay"}}

browser_wait_for: {"event": "navigation", "predicate": {"frame.url": "https://shop.example/order/confirmed"}, "timeout_ms": 10000}

task_render_pdf: {"target_id": "..."}
// Attach the PDF to your test report.
```

## CI integration

Run `netra-browser --launch --launch-headless --listen 127.0.0.1:0` in your CI job. The `--launch` mode spawns a fresh Chrome per job; `--profile-snapshot` keeps the source profile clean.
