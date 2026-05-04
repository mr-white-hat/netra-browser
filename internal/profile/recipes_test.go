package profile

import (
	"testing"
)

func TestSaveLoadRecipe(t *testing.T) {
	dir := t.TempDir()
	r := Recipe{
		Name: "login",
		Actions: []RecipeAction{
			{Tool: "browser_navigate", Args: map[string]any{"url": "https://x"}},
		},
	}
	if _, err := SaveRecipe(dir, r); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRecipe(dir, "login")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "login" || len(got.Actions) != 1 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt set")
	}
}

func TestListRecipes(t *testing.T) {
	dir := t.TempDir()
	if _, err := SaveRecipe(dir, Recipe{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRecipe(dir, Recipe{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	got, err := ListRecipes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2: %d", len(got))
	}
}

func TestListRecipesMissingDir(t *testing.T) {
	got, err := ListRecipes(t.TempDir() + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty: %d", len(got))
	}
}
