package database

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Category is a node of the catalogue tree. What a product belongs to is still
// the path in products.category — this row describes the node: the owner's text
// for its page, where it stands among its siblings and whether the storefront
// shows it at all. A node with no products of its own is a node the owner
// declared before filling it.
type Category struct {
	Path     string `json:"path"`
	Body     string `json:"body"`
	Position int    `json:"position"`
	Hidden   bool   `json:"hidden"`
	// Count is filled by Tree: how many visible products hang at or below the
	// node. Not stored — a counter kept in a column drifts from the truth.
	Count int `json:"count"`
	// LastMod is the date of the newest product below the node, "2006-01-02":
	// the sitemap needs one, and an ISO date compares as a string.
	LastMod string `json:"-"`
}

// ErrCategoryExists — two nodes cannot share a path, and two names that
// transliterate into one slug cannot share an address either.
var ErrCategoryExists = errors.New("category exists")

// ErrCategorySlugTaken — different names, one URL: "КПБ" and "К.П.Б." both
// become "kpb", and the second page would quietly replace the first.
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

// upsertCategory writes one column of a node, creating the row when the node
// only existed as a path on some products.
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

// CreateCategory declares a node that has no products yet. The storefront still
// ignores it until something is in it — an empty listing is a soft 404 — but the
// owner can build the tree first and fill it after.
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

// checkFree refuses a path already taken by a node or by a node whose slug is
// the same: the address is what matters, and two names may share one.
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

// CategoryPath joins segment names into a stored path. A slash inside a name
// becomes a dash: it would invent a level that is not there ("КПБ 1,5/2 сп" is
// one category, not two). Every writer of a category goes through here, so what
// a path is gets decided in one place.
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

// NormalizePath cleans a path that is already a path: empty segments and stray
// separators disappear, the levels stay levels.
func NormalizePath(path string) string {
	return CategoryPath(strings.Split(path, CategorySep)...)
}

// JoinCategory puts a new name under a parent path — the shape the admin works
// in: the parent is a path, the name is one level.
func JoinCategory(parent, name string) string {
	return CategoryPath(append(strings.Split(NormalizePath(parent), CategorySep), name)...)
}

// SlugPath is the storefront address of a node: every segment transliterated.
// Kept here rather than in the storefront because the admin has to refuse a
// name whose address is taken, and both must agree on what the address is.
func SlugPath(path string) string {
	segments := strings.Split(path, CategorySep)
	for i, seg := range segments {
		segments[i] = Slugify(seg)
	}
	return strings.Join(segments, CategorySep)
}

// Tree returns every node: the ones products live in and the ones the owner
// declared, each with the number of visible products at or below it. Parents
// are folded in, so "Текстиль/Спальня/КПБ" also yields "Текстиль" and
// "Текстиль/Спальня" even when nothing sits on those levels directly.
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
		// A declared node exists on its own, and so do its parents: a child
		// without a parent in the tree cannot be drawn.
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

// sortCategories orders siblings by the owner's position first and by name
// second, and keeps the whole list in tree order so a caller can draw it
// without sorting again.
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

// VisibleCategories is what the storefront may show: hidden nodes and
// everything below them are gone, and so are nodes with no goods — an empty
// listing is a soft 404 and spends the crawl budget on nothing.
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
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// RenameCategory moves a node — a rename and a re-parent are the same operation
// on a path. Products and descendants travel with it in one transaction, and
// the old address is remembered so a page that already earns search traffic
// answers 301 instead of 404.
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

// DeleteCategory lifts everything one level up: products and subcategories move
// to the parent, a root node's goods become uncategorised. Deleting a shelf is
// not deleting what stands on it.
func (d *Database) DeleteCategory(path string) error {
	path = NormalizePath(path)
	if path == "" {
		return fmt.Errorf("empty path")
	}
	parent := ParentPath(path)
	return d.withTx(func(tx *sql.Tx) error {
		// The row goes before the move, not after: renaming it to the parent's
		// path would collide with the parent's own row.
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

// movePath rewrites the prefix everywhere it is stored: on the products, on the
// nodes and in the redirects that already pointed at the old address. Deleting a
// root node moves its goods to "" — they keep selling, they just lose a shelf.
func movePath(tx *sql.Tx, from, to string) error {
	// substr() is 1-based and counts characters, not bytes: a Cyrillic prefix is
	// twice as long in bytes, and len() here would cut the path in the middle.
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
	// The node's own row travels with it. No collision is possible here: a
	// rename checks the destination first, and a delete drops the row before
	// moving what is left.
	if err := rewrite("categories", "path"); err != nil {
		return err
	}
	if err := rewrite("category_redirects", "new_path"); err != nil {
		return err
	}
	// The old address must keep working: it is in the index and in somebody's
	// bookmarks. An empty destination means the goods lost their category, and
	// then the page is gone for good.
	if to == "" {
		_, err := tx.Exec(`DELETE FROM category_redirects WHERE old_path=?`, from)
		return err
	}
	_, err := tx.Exec(
		`INSERT INTO category_redirects (old_path, new_path) VALUES (?, ?)
		 ON CONFLICT(old_path) DO UPDATE SET new_path=excluded.new_path`, from, to)
	return err
}

// CategoryRedirectBySlug answers where a renamed category went, looked up by
// the address a visitor asked for, and returns the new address. Only the moved
// node itself is recorded, so a child is matched by prefix: renaming
// "Текстиль" must also move "Текстиль/КПБ", which is where the traffic is.
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
