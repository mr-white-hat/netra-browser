package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Recipe is the on-disk shape of a saved interaction sequence.
type Recipe struct {
	Name             string         `json:"name"`
	TargetPattern    string         `json:"target_pattern,omitempty"`
	Actions          []RecipeAction `json:"actions"`
	SuccessMarker    map[string]any `json:"success_marker,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	FirstSucceededAt *time.Time     `json:"first_succeeded_at,omitempty"`
	LastUsedAt       *time.Time     `json:"last_used_at,omitempty"`
}

// RecipeAction is one step (tool name + args).
type RecipeAction struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

// DefaultRecipesDir returns ~/.config/netra-browser/recipes.
func DefaultRecipesDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "netra-browser", "recipes"), nil
}

// SaveRecipe writes the recipe under dir as <name>.json.
func SaveRecipe(dir string, r Recipe) (string, error) {
	if r.Name == "" {
		return "", fmt.Errorf("recipe name required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	path := filepath.Join(dir, r.Name+".json")
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// LoadRecipe reads a single named recipe from dir.
func LoadRecipe(dir, name string) (*Recipe, error) {
	path := filepath.Join(dir, name+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Recipe
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListRecipes returns every recipe in dir, sorted by name.
func ListRecipes(dir string) ([]*Recipe, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Recipe
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		r, err := LoadRecipe(dir, trimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// TouchRecipe updates LastUsedAt and persists. Returns the path.
func TouchRecipe(dir, name string) error {
	r, err := LoadRecipe(dir, name)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	r.LastUsedAt = &now
	_, err = SaveRecipe(dir, *r)
	return err
}

func trimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}
