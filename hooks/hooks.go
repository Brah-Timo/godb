// Package hooks defines the lifecycle hook interfaces for godb models.
//
// # Philosophy
//
// godb uses interface-based hooks rather than callback registration.
// You implement an interface directly on your model struct; godb detects it
// at schema-parse time (once, via reflect.Type.Implements) and calls it on
// every relevant operation.
//
// This means:
//   - The compiler validates the signature at build time — no runtime surprises.
//   - Zero allocation overhead: no function pointer tables, no dynamic dispatch beyond a single type assertion.
//   - Easy to test: just call the method directly in your unit tests.
//
// # Available hooks
//
//	type BeforeCreater interface { BeforeCreate() error }
//	type AfterCreater  interface { AfterCreate() }
//	type BeforeUpdater interface { BeforeUpdate() error }
//	type AfterUpdater  interface { AfterUpdate() }
//	type BeforeDeleter interface { BeforeDelete() error }
//	type AfterDeleter  interface { AfterDelete() }
//	type Validator     interface { Validate() error }
//
// # Example
//
//	type User struct {
//	    ID       uint   `godb:"primary_key;auto_increment"`
//	    Email    string `godb:"unique;not_null"`
//	    Password string
//	}
//
//	func (u *User) BeforeCreate() error {
//	    if len(u.Password) < 8 {
//	        return errors.New("password too short")
//	    }
//	    hashed, err := bcrypt.GenerateFromPassword([]byte(u.Password), 12)
//	    if err != nil {
//	        return err
//	    }
//	    u.Password = string(hashed)
//	    return nil
//	}
//
//	func (u *User) AfterCreate() {
//	    go sendWelcomeEmail(u.Email)
//	}
//
//	func (u *User) Validate() error {
//	    if !isValidEmail(u.Email) {
//	        return errors.New("invalid email")
//	    }
//	    return nil
//	}
package hooks

// BeforeCreater is called before every INSERT.
// Return a non-nil error to abort the operation.
type BeforeCreater interface {
	BeforeCreate() error
}

// AfterCreater is called after a successful INSERT.
// Runs synchronously in the same goroutine as the caller.
type AfterCreater interface {
	AfterCreate()
}

// BeforeUpdater is called before every UPDATE.
// Return a non-nil error to abort the operation.
type BeforeUpdater interface {
	BeforeUpdate() error
}

// AfterUpdater is called after a successful UPDATE.
type AfterUpdater interface {
	AfterUpdate()
}

// BeforeDeleter is called before every hard-delete or soft-delete.
// Return a non-nil error to abort the operation.
type BeforeDeleter interface {
	BeforeDelete() error
}

// AfterDeleter is called after a successful delete.
type AfterDeleter interface {
	AfterDelete()
}

// Validator is called before any write operation (Create, Save, Update).
// It runs before BeforeCreate / BeforeUpdate, so it is the right place
// for business-rule validation that is independent of operation type.
// Return a non-nil error to abort.
type Validator interface {
	Validate() error
}

// ─────────────────────────────────────────────────────────────
//  Runner helpers (used by the builder)
// ─────────────────────────────────────────────────────────────

// RunBeforeCreate invokes BeforeCreate on value if implemented.
func RunBeforeCreate(value interface{}) error {
	if h, ok := value.(BeforeCreater); ok {
		return h.BeforeCreate()
	}
	return nil
}

// RunAfterCreate invokes AfterCreate on value if implemented.
func RunAfterCreate(value interface{}) {
	if h, ok := value.(AfterCreater); ok {
		h.AfterCreate()
	}
}

// RunBeforeUpdate invokes BeforeUpdate on value if implemented.
func RunBeforeUpdate(value interface{}) error {
	if h, ok := value.(BeforeUpdater); ok {
		return h.BeforeUpdate()
	}
	return nil
}

// RunAfterUpdate invokes AfterUpdate on value if implemented.
func RunAfterUpdate(value interface{}) {
	if h, ok := value.(AfterUpdater); ok {
		h.AfterUpdate()
	}
}

// RunBeforeDelete invokes BeforeDelete on value if implemented.
func RunBeforeDelete(value interface{}) error {
	if h, ok := value.(BeforeDeleter); ok {
		return h.BeforeDelete()
	}
	return nil
}

// RunAfterDelete invokes AfterDelete on value if implemented.
func RunAfterDelete(value interface{}) {
	if h, ok := value.(AfterDeleter); ok {
		h.AfterDelete()
	}
}

// RunValidate invokes Validate on value if implemented.
func RunValidate(value interface{}) error {
	if h, ok := value.(Validator); ok {
		return h.Validate()
	}
	return nil
}
