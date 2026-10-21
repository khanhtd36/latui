package teams

import (
	"encoding/json"
	"errors"

	"github.com/zalando/go-keyring"
)

// Login is what the Teams lookup signs in with: the account's email and an
// API token.
type Login struct {
	Email string `json:"email,omitempty"` // empty: the sign-in's own email
	Token string `json:"token"`
}

// Store keeps a Login, one per site.
type Store interface {
	Load(site string) (Login, bool, error)
	Save(site string, l Login) error
	Delete(site string) error
}

// KeyringStore keeps logins in the OS secure store (Windows Credential
// Manager, macOS Keychain, Linux Secret Service), never in a file. The
// funcs are go-keyring's unless a test replaces them.
type KeyringStore struct {
	Get    func(service, user string) (string, error)
	Set    func(service, user, password string) error
	Remove func(service, user string) error
}

func (s KeyringStore) funcs() KeyringStore {
	if s.Get == nil {
		s.Get = keyring.Get
	}
	if s.Set == nil {
		s.Set = keyring.Set
	}
	if s.Remove == nil {
		s.Remove = keyring.Delete
	}
	return s
}

const keyUser = "token"

func service(site string) string { return "latui:teams:" + site }

func (s KeyringStore) Load(site string) (Login, bool, error) {
	raw, err := s.funcs().Get(service(site), keyUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return Login{}, false, nil
	}
	if err != nil {
		return Login{}, false, err
	}
	var l Login
	if err := json.Unmarshal([]byte(raw), &l); err != nil || l.Token == "" {
		return Login{}, false, nil // not ours or damaged: as if nothing were stored
	}
	return l, true, nil
}

func (s KeyringStore) Save(site string, l Login) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if err := s.funcs().Set(service(site), keyUser, string(b)); err != nil {
		return ErrNoSecureStore{err}
	}
	return nil
}

func (s KeyringStore) Delete(site string) error {
	err := s.funcs().Remove(service(site), keyUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// ErrNoSecureStore is a failure to write the OS secure store, such as on a
// machine that has none.
type ErrNoSecureStore struct{ Err error }

func (e ErrNoSecureStore) Error() string {
	return "couldn't write the OS secure store (" + e.Err.Error() + "): set LATUI_TEAMS_TOKEN instead"
}

func (e ErrNoSecureStore) Unwrap() error { return e.Err }
