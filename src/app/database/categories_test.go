package database

import (
	"slices"
	"strings"
	"testing"
)

func treeDB(t *testing.T, categories ...string) *Database {
	t.Helper()
	d, err := OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	for i, c := range categories {
		p := &Product{Title: "Товар " + string(rune('A'+i)), Price: 100, Category: c}
		if err := d.CreateProduct(p); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func categoryOf(t *testing.T, d *Database, title string) string {
	t.Helper()
	products, err := d.ListProducts()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range products {
		if p.Title == title {
			return p.Category
		}
	}
	t.Fatalf("no product %q", title)
	return ""
}

func TestRenameCategory(t *testing.T) {
	d := treeDB(t,
		"Текстиль",                     // A
		"Текстиль/Спальня/КПБ",         // B
		"Текстильная галантерея",       // C
		"Текстильная галантерея/Ленты") // D

	if err := d.RenameCategory("Текстиль", "Домашний текстиль"); err != nil {
		t.Fatal(err)
	}
	for title, want := range map[string]string{
		"Товар A": "Домашний текстиль",
		"Товар B": "Домашний текстиль/Спальня/КПБ",
		"Товар C": "Текстильная галантерея",
		"Товар D": "Текстильная галантерея/Ленты",
	} {
		if got := categoryOf(t, d, title); got != want {
			t.Errorf("%s: category = %q, want %q", title, got, want)
		}
	}

	// The old addresses must move, not die: the node itself and every page under it.
	for from, want := range map[string]string{
		"tekstil":         "domashnij-tekstil",
		"tekstil/spalnya": "domashnij-tekstil/spalnya",
	} {
		to, ok, err := d.CategoryRedirectBySlug(from)
		if err != nil || !ok || to != want {
			t.Errorf("redirect %q = %q %v %v, want %q", from, to, ok, err, want)
		}
	}
}

// Moving a node under another parent is the same operation as renaming it.
func TestMoveCategoryUnderAnotherParent(t *testing.T) {
	d := treeDB(t, "Посуда/Кастрюли", "Кухня")
	if err := d.RenameCategory("Посуда/Кастрюли", "Кухня/Кастрюли"); err != nil {
		t.Fatal(err)
	}
	if got := categoryOf(t, d, "Товар A"); got != "Кухня/Кастрюли" {
		t.Errorf("category = %q", got)
	}
	if err := d.RenameCategory("Кухня", "Кухня/Внутрь себя"); err == nil {
		t.Error("a category must not be moved into itself")
	}
}

// Deleting a shelf is not deleting what stands on it.
func TestDeleteCategoryLiftsGoodsUp(t *testing.T) {
	d := treeDB(t, "Текстиль/Спальня", "Текстиль/Спальня/КПБ", "Посуда")

	if err := d.DeleteCategory("Текстиль/Спальня"); err != nil {
		t.Fatal(err)
	}
	if got := categoryOf(t, d, "Товар A"); got != "Текстиль" {
		t.Errorf("goods of a deleted category went to %q, want Текстиль", got)
	}
	if got := categoryOf(t, d, "Товар B"); got != "Текстиль/КПБ" {
		t.Errorf("subcategory went to %q, want Текстиль/КПБ", got)
	}

	// A root node has nowhere to lift to: its goods lose the category.
	if err := d.DeleteCategory("Посуда"); err != nil {
		t.Fatal(err)
	}
	if got := categoryOf(t, d, "Товар C"); got != "" {
		t.Errorf("goods of a deleted root category went to %q, want none", got)
	}
}

// Two names, one address: the second page would quietly replace the first.
func TestCategoryAddressCollision(t *testing.T) {
	d := treeDB(t, "КПБ")
	// «КПБ» in quotes is another name with the same address: quotes vanish in a slug.
	if err := d.CreateCategory("«КПБ»"); err != ErrCategorySlugTaken {
		t.Errorf("create with a taken slug: %v", err)
	}
	if err := d.CreateCategory("КПБ"); err != ErrCategoryExists {
		t.Errorf("create with an existing path: %v", err)
	}
	// A rename of a node onto its own address is not a collision with itself.
	if err := d.RenameCategory("КПБ", "КПБ Евро"); err != nil {
		t.Errorf("rename: %v", err)
	}
}

func TestDeclaredCategoryIsAdminOnly(t *testing.T) {
	d := treeDB(t, "Посуда")
	if err := d.CreateCategory("Мебель/Стулья"); err != nil {
		t.Fatal(err)
	}
	tree, err := d.Tree()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, n := range tree {
		paths = append(paths, n.Path)
	}
	for _, want := range []string{"Мебель", "Мебель/Стулья"} {
		if !slices.Contains(paths, want) {
			t.Errorf("tree has no %q: %v", want, paths)
		}
	}
	visible, err := d.VisibleCategories()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range visible {
		if strings.HasPrefix(n.Path, "Мебель") {
			t.Errorf("an empty category reached the storefront: %q", n.Path)
		}
	}
}

func TestHiddenCategoryHidesItsBranch(t *testing.T) {
	d := treeDB(t, "Распродажа/Уценка", "Посуда")
	if err := d.SetCategoryHidden("Распродажа", true); err != nil {
		t.Fatal(err)
	}
	visible, err := d.VisibleCategories()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range visible {
		if strings.HasPrefix(n.Path, "Распродажа") {
			t.Errorf("hidden branch reached the storefront: %q", n.Path)
		}
	}
	if len(visible) != 1 || visible[0].Path != "Посуда" {
		t.Errorf("visible = %+v", visible)
	}
}

// Position decides the order of siblings; the name only breaks a tie.
func TestCategoryPosition(t *testing.T) {
	d := treeDB(t, "Аксессуары", "Посуда")
	if err := d.SetCategoryPosition("Посуда", -1); err != nil {
		t.Fatal(err)
	}
	tree, err := d.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if tree[0].Path != "Посуда" {
		t.Errorf("order = %q, %q; position must win over the name", tree[0].Path, tree[1].Path)
	}
}
