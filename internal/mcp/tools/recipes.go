package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
	"github.com/mr-white-hat/netra-browser/internal/profile"
)

// RegisterRecipes installs task_record_recipe / task_replay_recipe / task_list_recipes.
//
// dir is where recipe JSON files live (default: ~/.config/netra-browser/recipes).
func RegisterRecipes(reg *mcp.Registry, sess *mcp.Session) {
	dir, _ := profile.DefaultRecipesDir()
	RegisterRecipesAt(reg, sess, dir)
}

// RegisterRecipesAt is the test-friendly variant that takes an explicit dir.
func RegisterRecipesAt(reg *mcp.Registry, sess *mcp.Session, dir string) {
	reg.Register("task_record_recipe", func(ctx context.Context, params json.RawMessage) (any, error) {
		var args struct {
			Name          string                 `json:"name"`
			Actions       []profile.RecipeAction `json:"actions"`
			SuccessMarker map[string]any         `json:"success_marker"`
			TargetID      string                 `json:"target_id"`
			TargetPattern string                 `json:"target_pattern"`
		}
		if err := json.Unmarshal(params, &args); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		if args.Name == "" || len(args.Actions) == 0 {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "name and actions required"}.AsResult(), nil
		}
		r := profile.Recipe{
			Name:          args.Name,
			Actions:       args.Actions,
			SuccessMarker: args.SuccessMarker,
			TargetPattern: args.TargetPattern,
		}
		path, err := profile.SaveRecipe(dir, r)
		if err != nil {
			return mcp.ToolError{Code: "io_error", Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "recipe_path": path}, nil
	})

	reg.Register("task_replay_recipe", func(ctx context.Context, params json.RawMessage) (any, error) {
		var args struct {
			Name     string            `json:"name"`
			TargetID string            `json:"target_id"`
			Env      map[string]string `json:"env"`
		}
		if err := json.Unmarshal(params, &args); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		if args.Name == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "name required"}.AsResult(), nil
		}
		r, err := profile.LoadRecipe(dir, args.Name)
		if err != nil {
			if os.IsNotExist(err) {
				return mcp.ToolError{Code: mcp.ErrNotFound, Message: "recipe not found: " + args.Name}.AsResult(), nil
			}
			return mcp.ToolError{Code: "io_error", Message: err.Error()}.AsResult(), nil
		}

		envCopy := map[string]string{}
		for k, v := range args.Env {
			envCopy[k] = v
		}

		var lastResult any
		stepsDone := 0
		for _, step := range r.Actions {
			subbed, err := substituteVars(step.Args, envCopy)
			if err != nil {
				return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
			}
			if args.TargetID != "" {
				if _, ok := subbed["target_id"]; !ok {
					subbed["target_id"] = args.TargetID
				}
			}
			b, _ := json.Marshal(subbed)
			res, err := reg.Invoke(ctx, step.Tool, b)
			if err != nil {
				return mcp.ToolError{Code: "step_failed", Message: err.Error()}.AsResult(), nil
			}
			lastResult = res
			stepsDone++
		}

		successVerified := false
		if r.SuccessMarker != nil {
			successVerified = checkSuccessMarker(ctx, reg, args.TargetID, r.SuccessMarker)
		}
		_ = profile.TouchRecipe(dir, r.Name)

		return map[string]any{
			"ok":               true,
			"steps_executed":   stepsDone,
			"last_step_result": lastResult,
			"success_verified": successVerified,
		}, nil
	})

	reg.Register("task_list_recipes", func(ctx context.Context, _ json.RawMessage) (any, error) {
		recipes, err := profile.ListRecipes(dir)
		if err != nil {
			return mcp.ToolError{Code: "io_error", Message: err.Error()}.AsResult(), nil
		}
		out := make([]map[string]any, 0, len(recipes))
		for _, r := range recipes {
			row := map[string]any{
				"name":       r.Name,
				"step_count": len(r.Actions),
				"created_at": r.CreatedAt,
			}
			if r.TargetPattern != "" {
				row["target_pattern"] = r.TargetPattern
			}
			if r.LastUsedAt != nil {
				row["last_used_at"] = r.LastUsedAt
			}
			out = append(out, row)
		}
		return map[string]any{"ok": true, "recipes": out}, nil
	})
}

// substituteVars walks the args tree replacing $VAR tokens in string values
// with env[VAR]. Non-string values pass through. Missing vars return an error
// so the agent finds out before partially executing the recipe.
func substituteVars(in map[string]any, env map[string]string) (map[string]any, error) {
	out := make(map[string]any, len(in))
	for k, v := range in {
		got, err := substituteValue(v, env)
		if err != nil {
			return nil, err
		}
		out[k] = got
	}
	return out, nil
}

func substituteValue(v any, env map[string]string) (any, error) {
	switch x := v.(type) {
	case string:
		return substituteString(x, env)
	case map[string]any:
		return substituteVars(x, env)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			got, err := substituteValue(item, env)
			if err != nil {
				return nil, err
			}
			out[i] = got
		}
		return out, nil
	default:
		return v, nil
	}
}

func substituteString(s string, env map[string]string) (string, error) {
	if !strings.Contains(s, "$") {
		return s, nil
	}
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '$' {
			b.WriteByte(s[i])
			i++
			continue
		}
		// Find end of var name (alnum + underscore).
		j := i + 1
		for j < len(s) && (isAlphaNum(s[j]) || s[j] == '_') {
			j++
		}
		if j == i+1 {
			b.WriteByte('$')
			i++
			continue
		}
		name := s[i+1 : j]
		val, ok := env[name]
		if !ok {
			return "", fmt.Errorf("recipe variable not provided: $%s", name)
		}
		b.WriteString(val)
		i = j
	}
	return b.String(), nil
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// checkSuccessMarker re-invokes browser_eval / browser_get_recent_events to verify
// a marker landed (best effort: return false on any error).
func checkSuccessMarker(ctx context.Context, reg *mcp.Registry, targetID string, marker map[string]any) bool {
	// Marker by URL pattern: re-read location.href via browser_eval and substring-match.
	if pat, ok := marker["url_pattern"].(string); ok && pat != "" {
		args := map[string]any{"expression": "location.href"}
		if targetID != "" {
			args["target_id"] = targetID
		}
		b, _ := json.Marshal(args)
		res, err := reg.Invoke(ctx, "browser_eval", b)
		if err != nil {
			return false
		}
		m, ok := res.(map[string]any)
		if !ok {
			return false
		}
		s, _ := m["result"].(string)
		return strings.Contains(s, pat)
	}
	// Marker by text: re-read body innerText via browser_eval and substring-match.
	if text, ok := marker["text"].(string); ok && text != "" {
		args := map[string]any{"expression": "document.body && document.body.innerText"}
		if targetID != "" {
			args["target_id"] = targetID
		}
		b, _ := json.Marshal(args)
		res, err := reg.Invoke(ctx, "browser_eval", b)
		if err != nil {
			return false
		}
		m, ok := res.(map[string]any)
		if !ok {
			return false
		}
		s, _ := m["result"].(string)
		return strings.Contains(s, text)
	}
	return false
}
