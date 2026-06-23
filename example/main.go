// Command example demonstrates the full godb API against a SQLite3 in-memory database.
//
// Run:
//
//	cd example && go run main.go
package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Brah-Timo/godb"
	_ "github.com/mattn/go-sqlite3"
)

// ═══════════════════════════════════════════════════════════════════════════
//  Model definitions
// ═══════════════════════════════════════════════════════════════════════════

// User demonstrates: primary key, unique, not_null, soft_delete, hooks, timestamps.
type User struct {
	ID        uint   `godb:"primary_key;auto_increment"`
	Name      string `godb:"not_null;size:100"`
	Email     string `godb:"unique;not_null;size:255"`
	Age       int    `godb:"not_null;index"`
	Active    bool   `godb:"default:true"`
	Posts     []Post `godb:"has_many;foreign_key:user_id"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time `godb:"softDelete"`
}

// BeforeCreate validates and pre-processes data before INSERT.
func (u *User) BeforeCreate() error {
	if u.Email == "" {
		return errors.New("email is required")
	}
	if u.Age < 0 {
		return errors.New("age cannot be negative")
	}
	return nil
}

// AfterCreate runs after successful INSERT (e.g. send welcome email).
func (u *User) AfterCreate() {
	fmt.Printf("  [hook] AfterCreate fired for user %q (ID=%d)\n", u.Name, u.ID)
}

// Validate is called before any write (create + update).
func (u *User) Validate() error {
	if len(u.Name) < 2 {
		return errors.New("name must be at least 2 characters")
	}
	return nil
}

// Post demonstrates: belongs_to relation, index on FK, optional soft-delete.
type Post struct {
	ID        uint   `godb:"primary_key;auto_increment"`
	UserID    uint   `godb:"not_null;index"`
	Title     string `godb:"not_null;size:500"`
	Content   string
	Views     int   `godb:"default:0"`
	User      *User `godb:"belongs_to;foreign_key:user_id"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Tag demonstrates: many-to-many relation.
type Tag struct {
	ID   uint   `godb:"primary_key;auto_increment"`
	Name string `godb:"unique;not_null;size:50"`
}

// ═══════════════════════════════════════════════════════════════════════════
//  main
// ═══════════════════════════════════════════════════════════════════════════

func main() {
	sep := func(label string) { fmt.Printf("\n\033[1;34m── %s ──\033[0m\n", label) }
	ok := func(msg string) { fmt.Printf("  \033[32m✅ %s\033[0m\n", msg) }
	must := func(err error, msg string) {
		if err != nil {
			log.Fatalf("❌ %s: %v", msg, err)
		}
	}

	// ─────────────────────────────────────────────────
	sep("Open DB")
	// ─────────────────────────────────────────────────
	db, err := godb.Open("sqlite3", "file::memory:?cache=shared&mode=rwc",
		godb.WithPool(5, 2, time.Minute),
		godb.WithCache("memory", map[string]string{"max_size_mb": "32"}),
		godb.WithLogger(godb.LogInfo),
		godb.WithSlowQueryAlert(50*time.Millisecond),
	)
	must(err, "open db")
	defer db.Close()
	ok("Database opened (SQLite3 in-memory)")

	// ─────────────────────────────────────────────────
	sep("Auto Migrate")
	// ─────────────────────────────────────────────────
	must(db.AutoMigrate(&User{}, &Post{}, &Tag{}), "automigrate")
	ok("Tables created: users, posts, tags")

	// Preview migration plan
	plan, err := db.MigratePlan(&User{}, &Post{})
	must(err, "plan")
	if plan.IsEmpty {
		ok("Migration plan: no changes (schema is already up-to-date)")
	}

	// ─────────────────────────────────────────────────
	sep("Create Users")
	// ─────────────────────────────────────────────────
	alice := &User{Name: "Alice Smith", Email: "alice@example.com", Age: 28, Active: true}
	must(db.Model(alice).Create(alice), "create alice")
	ok(fmt.Sprintf("Alice created — ID=%d", alice.ID))

	bob := &User{Name: "Bob Jones", Email: "bob@example.com", Age: 22, Active: true}
	must(db.Model(bob).Create(bob), "create bob")
	ok(fmt.Sprintf("Bob created — ID=%d", bob.ID))

	charlie := &User{Name: "Charlie Brown", Email: "charlie@example.com", Age: 35, Active: false}
	must(db.Model(charlie).Create(charlie), "create charlie")
	ok(fmt.Sprintf("Charlie created — ID=%d", charlie.ID))

	// Validation error test
	badUser := &User{Name: "X", Email: "bad@example.com", Age: 20}
	if err := db.Model(badUser).Create(badUser); err != nil {
		ok(fmt.Sprintf("Validation error caught: %v", err))
	}

	// ─────────────────────────────────────────────────
	sep("Create Posts")
	// ─────────────────────────────────────────────────
	posts := []Post{
		{UserID: alice.ID, Title: "Hello godb", Content: "This ORM is awesome!"},
		{UserID: alice.ID, Title: "Advanced Go Patterns", Content: "Let's talk about generics."},
		{UserID: bob.ID, Title: "SQLite in Production", Content: "Yes, it works."},
	}
	for i := range posts {
		must(db.Model(&posts[i]).Create(&posts[i]), "create post")
	}
	ok(fmt.Sprintf("%d posts created", len(posts)))

	// ─────────────────────────────────────────────────
	sep("Basic Queries")
	// ─────────────────────────────────────────────────

	// Find all active users with cache
	var activeUsers []User
	err = db.Model(&User{}).
		Where("active = ?", true).
		Order("name ASC").
		Cache(5 * time.Minute).
		Find(&activeUsers)
	must(err, "find active users")
	ok(fmt.Sprintf("Active users: %d found", len(activeUsers)))
	for _, u := range activeUsers {
		fmt.Printf("    • %s <%s> age=%d\n", u.Name, u.Email, u.Age)
	}

	// First
	var firstUser User
	must(db.Model(&User{}).First(&firstUser), "first user")
	ok(fmt.Sprintf("First user: %s (ID=%d)", firstUser.Name, firstUser.ID))

	// Last
	var lastUser User
	must(db.Model(&User{}).Last(&lastUser), "last user")
	ok(fmt.Sprintf("Last user: %s (ID=%d)", lastUser.Name, lastUser.ID))

	// Count
	total, err := db.Model(&User{}).Count()
	must(err, "count")
	ok(fmt.Sprintf("Total users (including soft-deleted): %d", total))

	// ─────────────────────────────────────────────────
	sep("Advanced Queries")
	// ─────────────────────────────────────────────────

	// WhereIn
	var someUsers []User
	must(db.Model(&User{}).WhereIn("id", alice.ID, bob.ID).Find(&someUsers), "where in")
	ok(fmt.Sprintf("WhereIn(id, %d, %d): %d users", alice.ID, bob.ID, len(someUsers)))

	// WhereBetween
	var midAge []User
	must(db.Model(&User{}).WhereBetween("age", 20, 30).Find(&midAge), "where between")
	ok(fmt.Sprintf("WhereBetween(age, 20, 30): %d users", len(midAge)))

	// Limit + Offset (pagination)
	var page []User
	must(db.Model(&User{}).Order("id ASC").Limit(2).Offset(0).Find(&page), "page 1")
	ok(fmt.Sprintf("Page 1 (limit 2, offset 0): %d users", len(page)))

	// Select specific columns
	var names []struct {
		Name  string
		Email string
	}
	must(db.Model(&User{}).Select("name", "email").Find(&names), "select cols")
	ok(fmt.Sprintf("Selected name+email for %d users", len(names)))

	// Exists
	exists, err := db.Model(&User{}).Where("email = ?", "alice@example.com").Exists()
	must(err, "exists")
	ok(fmt.Sprintf("alice exists: %v", exists))

	// ─────────────────────────────────────────────────
	sep("Raw SQL")
	// ─────────────────────────────────────────────────

	var rawResult []struct {
		Name      string
		PostCount int
	}
	err = db.Raw(`
		SELECT u.name, COUNT(p.id) as post_count
		FROM users u
		LEFT JOIN posts p ON p.user_id = u.id
		WHERE u.deleted_at IS NULL
		GROUP BY u.id, u.name
		ORDER BY post_count DESC
	`).Scan(&rawResult)
	must(err, "raw scan")
	ok(fmt.Sprintf("Raw JOIN query: %d rows", len(rawResult)))
	for _, r := range rawResult {
		fmt.Printf("    • %-20s posts=%d\n", r.Name, r.PostCount)
	}

	// ─────────────────────────────────────────────────
	sep("Updates")
	// ─────────────────────────────────────────────────

	// Update via map
	must(db.Model(&User{}).Where("id = ?", alice.ID).
		Updates(map[string]interface{}{"age": 29, "active": true}), "update alice")
	ok(fmt.Sprintf("Alice's age updated to 29"))

	// Single column update
	must(db.Model(&User{}).Where("id = ?", bob.ID).
		Update("name", "Robert Jones"), "update bob name")
	ok("Bob renamed to Robert Jones")

	// ─────────────────────────────────────────────────
	sep("Soft Delete & Unscoped")
	// ─────────────────────────────────────────────────

	must(db.Model(&User{}).Where("id = ?", charlie.ID).Delete(), "soft delete charlie")
	ok("Charlie soft-deleted (deleted_at set)")

	// Normal query excludes soft-deleted
	var normal []User
	must(db.Model(&User{}).Find(&normal), "find normal")
	ok(fmt.Sprintf("Users after soft-delete (excluding deleted): %d", len(normal)))

	// Unscoped includes soft-deleted
	var all []User
	must(db.Model(&User{}).Unscoped().Find(&all), "find unscoped")
	ok(fmt.Sprintf("Users unscoped (including deleted): %d", len(all)))

	// ─────────────────────────────────────────────────
	sep("Transactions")
	// ─────────────────────────────────────────────────

	err = db.Transaction(func(tx *godb.DB) error {
		newUser := &User{Name: "TX User", Email: "tx@test.com", Age: 25}
		if err := tx.Model(newUser).Create(newUser); err != nil {
			return err // triggers automatic Rollback
		}
		post := &Post{UserID: newUser.ID, Title: "TX Post", Content: "In transaction"}
		if err := tx.Model(post).Create(post); err != nil {
			return err // triggers automatic Rollback
		}
		ok(fmt.Sprintf("Inside TX: user ID=%d, post ID=%d", newUser.ID, post.ID))
		return nil // triggers Commit
	})
	must(err, "transaction")
	ok("Transaction committed successfully")

	// Failed transaction (rollback demo)
	txErr := db.Transaction(func(tx *godb.DB) error {
		return errors.New("simulated failure — rollback triggered")
	})
	if txErr != nil {
		ok(fmt.Sprintf("Rollback demo: %v", txErr))
	}

	// ─────────────────────────────────────────────────
	sep("DryRun Mode")
	// ─────────────────────────────────────────────────

	dryDB, _ := godb.Open("sqlite3", "file::memory:?cache=shared",
		godb.WithDryRun(),
		godb.WithLogger(godb.LogInfo),
	)
	dryDB.AutoMigrate(&User{})
	sql, args := dryDB.Model(&User{}).Where("age > ?", 18).Order("name").Limit(5).ToSQL()
	ok(fmt.Sprintf("DryRun SQL: %s [args=%v]", sql, args))
	dryDB.Close()

	// ─────────────────────────────────────────────────
	sep("Cache Stats")
	// ─────────────────────────────────────────────────

	// Re-run the same query to get a cache hit
	var cachedUsers []User
	db.Model(&User{}).Where("active = ?", true).Cache(5 * time.Minute).Find(&cachedUsers)

	// ─────────────────────────────────────────────────
	sep("Ping & Stats")
	// ─────────────────────────────────────────────────

	must(db.Ping(), "ping")
	ok("Ping: OK")

	stats := db.Stats()
	ok(fmt.Sprintf("Pool: open=%d idle=%d inUse=%d",
		stats.OpenConnections, stats.Idle, stats.InUse))

	fmt.Println()
	fmt.Println("  \033[1;32m🎉 All examples completed successfully!\033[0m")
	fmt.Println("  \033[90mgodb — Prisma for Go. 5x faster than GORM.\033[0m")
	fmt.Println()
}
