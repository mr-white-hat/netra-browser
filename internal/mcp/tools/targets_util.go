package tools

import (
	"context"
	"encoding/json"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

// targetExists asks Chrome for the live target list and reports whether tid is in it.
// Used by tools that need to fail-fast against stale active-target IDs (Plan G hotfix).
func targetExists(ctx context.Context, client mcp.CDPSender, tid string) (bool, error) {
	if client == nil || tid == "" {
		return false, nil
	}
	raw, err := client.Send(ctx, "Target.getTargets", nil)
	if err != nil {
		return false, err
	}
	var resp struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return false, err
	}
	for _, ti := range resp.TargetInfos {
		if ti.TargetID == tid {
			return true, nil
		}
	}
	return false, nil
}
