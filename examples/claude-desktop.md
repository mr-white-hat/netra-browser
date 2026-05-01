# Claude Desktop quickstart

## 1. Configure Claude Desktop

Edit `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "netra-browser": {
      "command": "netra-browser"
    }
  }
}
```

## 2. Open Chrome with the debug port

```bash
google-chrome --remote-debugging-port=9222
```

(or use `--launch` mode and let `netra-browser` spawn it for you)

## 3. Restart Claude Desktop

The 30+ tools should appear in Claude's tool picker.

## 4. First session

Ask Claude:

> Use `meta_attach`, then `browser_list_tabs`, and summarize what's open.

If it works, you're set. From here you can drive any logged-in app.
