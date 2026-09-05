package database

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Category describes a tree node; membership stays the path in products.category.
type Category struct {
	Path     string `json:"path"`
	Body     string `json:"body"`
	Position int    `json:"position"`
	Hidden   bool   `json:"hidden"`
	// Count is filled by Tree: visible products at or below the node, never stored.
	Count int `json:"count"`
	// LastMod is the newest product date below the node, "2006-01-02" (sorts as text).
	LastMod string `json:"-"`
}

// ErrCategoryExists - two nodes cannot share a path.
var ErrCategoryExists = errors.New("category exists")

// ErrCategorySlugTaken - two different names can transliterate into one slug.
var ErrCategorySlugTaken = errors.New("category slug taken")

func (d *Database) SetCategoryText(path, body string) error {
	return d.upsertCategory(path, "body", strings.TrimSpace(body))
}

func (d *Database) SetCategoryPosition(path string, position int) error {
	return d.upsertCategory(path, "position", position)
}

func (d *Database) SetCategoryHidden(path string, hidden bool) error {
	return d.upsertCategory(path, "hidden", hidden)
}

// upsertCategory writes one column, creating the row when the node had none.
func (d *Database) upsertCategory(path, column string, value any) error {
	_, err := d.db.Exec(
		`INSERT INTO categories (path, `+column+`) VALUES (?, ?)
		 ON CONFLICT(path) DO UPDATE SET `+column+`=excluded.`+column, path, value)
	return err
}

func (d *Database) CategoryTextOf(path string) (string, error) {
	var body string
	err := d.db.QueryRow(`SELECT body FROM categories WHERE path=?`, path).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return body, err
}

// CreateCategory declares an empty node; the storefront hides it until it has goods.
func (d *Database) CreateCategory(path string) error {
	path = NormalizePath(path)
	if path == "" {
		return fmt.Errorf("empty path")
	}
	if err := d.checkFree(path, ""); err != nil {
		return err
	}
	_, err := d.db.Exec(`INSERT INTO categories (path) VALUES (?)`, path)
	return err
}

// checkFree refuses a path taken by a node or by a node with the same slug.
func (d *Database) checkFree(path, movingFrom string) error {
	nodes, err := d.Tree()
	if err != nil {
		return err
	}
	want := SlugPath(path)
	for _, n := range nodes {
		if n.Path == movingFrom || strings.HasPrefix(n.Path, movingFrom+CategorySep) {
			continue // the node being moved and its own children
		}
		if n.Path == path {
			return ErrCategoryExists
		}
		if SlugPath(n.Path) == want {
			return ErrCategorySlugTaken
		}
	}
	return nil
}

// CategoryPath joins segments; a slash inside a name becomes a dash, not a level.
func CategoryPath(segments ...string) string {
	out := make([]string, 0, len(segments))
	for _, s := range segments {
		s = strings.TrimSpace(strings.ReplaceAll(s, CategorySep, "-"))
		if s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, CategorySep)
}

// NormalizePath drops empty segments and stray separators from an existing path.
func NormalizePath(path string) string {
	return CategoryPath(strings.Split(path, CategorySep)...)
}

// JoinCategory puts one new name under a parent path.
func JoinCategory(parent, name string) string {
	return CategoryPath(append(strings.Split(NormalizePath(parent), CategorySep), name)...)
}

// SlugPath is a node's storefront address; admin and storefront must agree on it.
func SlugPath(path string) string {
	segments := strings.Split(path, CategorySep)
	for i, seg := range segments {
		segments[i] = Slugify(seg)
	}
	return strings.Join(segments, CategorySep)
}

// Tree returns every node with its visible count; parents are folded in.
func (d *Database) Tree() ([]Category, error) {
	byPath := map[string]*Category{}
	node := func(path string) *Category {
		if c, ok := byPath[path]; ok {
			return c
		}
		c := &Category{Path: path}
		byPath[path] = c
		return c
	}

	rows, err := d.db.Query(`SELECT category, COUNT(*), substr(MAX(updated_at), 1, 10)
		 FROM products WHERE hidden=0 AND category<>'' GROUP BY category`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var path, last string
		var count int
		if err := rows.Scan(&path, &count, &last); err != nil {
			return nil, err
		}
		segments := strings.Split(path, CategorySep)
		for i := range segments {
			n := node(strings.Join(segments[:i+1], CategorySep))
			n.Count += count
			if last > n.LastMod {
				n.LastMod = last
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	declared, err := d.db.Query(`SELECT path, body, position, hidden FROM categories`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = declared.Close() }()
	for declared.Next() {
		var c Category
		if err := declared.Scan(&c.Path, &c.Body, &c.Position, &c.Hidden); err != nil {
			return nil, err
		}
		// A child without its parents in the tree cannot be drawn.
		segments := strings.Split(c.Path, CategorySep)
		for i := range segments {
			node(strings.Join(segments[:i+1], CategorySep))
		}
		n := node(c.Path)
		n.Body, n.Position, n.Hidden = c.Body, c.Position, c.Hidden
	}
	if err := declared.Err(); err != nil {
		return nil, err
	}

	out := make([]Category, 0, len(byPath))
	for _, c := range byPath {
		out = append(out, *c)
	}
	sortCategories(out)
	return out, nil
}

// sortCategories orders siblings by position then name, and the list in tree order.
func sortCategories(nodes []Category) {
	pos := map[string]int{}
	for _, n := range nodes {
		pos[n.Path] = n.Position
	}
	key := func(path string) string {
		segments := strings.Split(path, CategorySep)
		var b strings.Builder
		for i := range segments {
			prefix := strings.Join(segments[:i+1], CategorySep)
			// Zero-padded so 10 sorts after 9, and the name breaks the tie.
			fmt.Fprintf(&b, "%09d\x00%s\x00", pos[prefix], strings.ToLower(segments[i]))
		}
		return b.String()
	}
	sort.Slice(nodes, func(i, j int) bool { return key(nodes[i].Path) < key(nodes[j].Path) })
}

// VisibleCategories drops hidden branches and empty nodes: an empty listing is a 404.
func (d *Database) VisibleCategories() ([]Category, error) {
	nodes, err := d.Tree()
	if err != nil {
		return nil, err
	}
	var hidden []string
	out := make([]Category, 0, len(nodes))
	for _, n := range nodes {
		if n.Hidden {
			hidden = append(hidden, n.Path+CategorySep)
		}
		if n.Hidden || n.Count == 0 || underAny(n.Path, hidden) {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func underAny(path string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(path, p) })
}

// RenameCategory moves a node; the old path is kept so its page 301s instead of 404s.
func (d *Database) RenameCategory(from, to string) error {
	from, to = NormalizePath(from), NormalizePath(to)
	if from == "" || to == "" {
		return fmt.Errorf("empty path")
	}
	if from == to {
		return nil
	}
	// Moving a node under itself would detach the branch from the tree.
	if strings.HasPrefix(to, from+CategorySep) {
		return fmt.Errorf("cannot move a category into itself")
	}
	if err := d.checkFree(to, from); err != nil {
		return err
	}
	return d.withTx(func(tx *sql.Tx) error {
		return movePath(tx, from, to)
	})
}

// DeleteCategory lifts products and subcategories one level up instead of removing them.
func (d *Database) DeleteCategory(path string) error {
	path = NormalizePath(path)
	if path == "" {
		return fmt.Errorf("empty path")
	}
	parent := ParentPath(path)
	return d.withTx(func(tx *sql.Tx) error {
		// The row goes before the move: renaming it to the parent's path would collide.
		if _, err := tx.Exec(`DELETE FROM categories WHERE path=?`, path); err != nil {
			return err
		}
		return movePath(tx, path, parent)
	})
}

// ParentPath is everything above the last segment; "" for a root node.
func ParentPath(path string) string {
	if i := strings.LastIndex(path, CategorySep); i > 0 {
		return path[:i]
	}
	return ""
}

// movePath rewrites the path prefix on products, nodes and redirects; "" = no category.
func movePath(tx *sql.Tx, from, to string) error {
	// substr() is 1-based and counts characters: len() would cut a Cyrillic path.
	tail := len([]rune(from)) + 1
	rewrite := func(table, column string) error {
		var query string
		if to == "" {
			query = `UPDATE ` + table + ` SET ` + column + ` = ltrim(substr(` + column + `, ?), ?)`
		} else {
			query = `UPDATE ` + table + ` SET ` + column + ` = ? || substr(` + column + `, ?)`
		}
		query += ` WHERE ` + column + ` = ? OR ` + column + ` LIKE ? ESCAPE '\'`
		args := []any{to, tail, from, likeEscape(from) + CategorySep + `%`}
		if to == "" {
			args = []any{tail, CategorySep, from, likeEscape(from) + CategorySep + `%`}
		}
		_, err := tx.Exec(query, args...)
		return err
	}
	if err := rewrite("products", "category"); err != nil {
		return err
	}
	// No collision: a rename checks the destination, a delete drops the row first.
	if err := rewrite("categories", "path"); err != nil {
		return err
	}
	if err := rewrite("category_redirects", "new_path"); err != nil {
		return err
	}
	// With no destination the page is gone; otherwise the old address must keep working.
	if to == "" {
		_, err := tx.Exec(`DELETE FROM category_redirects WHERE old_path=?`, from)
		return err
	}
	_, err := tx.Exec(
		`INSERT INTO category_redirects (old_path, new_path) VALUES (?, ?)
		 ON CONFLICT(old_path) DO UPDATE SET new_path=excluded.new_path`, from, to)
	return err
}

// CategoryRedirectBySlug maps an old slug to the new one; children match by prefix.
func (d *Database) CategoryRedirectBySlug(slug string) (string, bool, error) {
	rows, err := d.db.Query(`SELECT old_path, new_path FROM category_redirects`)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var oldPath, newPath string
		if err := rows.Scan(&oldPath, &newPath); err != nil {
			return "", false, err
		}
		from, to := SlugPath(oldPath), SlugPath(newPath)
		if slug == from {
			return to, true, nil
		}
		if tail, ok := strings.CutPrefix(slug, from+CategorySep); ok {
			return to + CategorySep + tail, true, nil
		}
	}
	return "", false, rows.Err()
}
