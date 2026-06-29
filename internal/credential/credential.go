package credential

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"filippo.io/age"
	"filippo.io/age/agessh"
)

const fileVersion = 1

var (
	ErrNotFound        = errors.New("credential not found")
	ErrNoEncryptionKey = errors.New("no SSH key found for encryption")
	ErrCannotDecrypt   = errors.New("cannot decrypt credentials")
	ErrCorrupt         = errors.New("credential file corrupt")
)

type Store struct {
	path       string
	keyPaths   []string
	recipients []age.Recipient
	identities []age.Identity
	passphrase func() (string, error)
	passOnce   sync.Once
	passValue  string
	passErr    error
}

type Option func(*options)

type options struct {
	path        string
	keyPaths    []string
	keyPathsSet bool
	passphrase  func() (string, error)
}

type credentialFile struct {
	Version     int               `json:"version"`
	Credentials map[string]string `json:"credentials"`
}

func Open(opts ...Option) (*Store, error) {
	cfg := options{}
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.path == "" {
		path, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		cfg.path = path
	}
	if !cfg.keyPathsSet {
		cfg.keyPaths = defaultKeyPaths()
	}

	s := &Store{
		path:       cfg.path,
		keyPaths:   cfg.keyPaths,
		passphrase: cfg.passphrase,
	}
	s.discoverSSHKeys()
	return s, nil
}

func WithPath(path string) Option {
	return func(o *options) {
		o.path = path
	}
}

func WithPassphrase(fn func() (string, error)) Option {
	return func(o *options) {
		o.passphrase = fn
	}
}

func withKeyPaths(paths ...string) Option {
	return func(o *options) {
		o.keyPaths = append([]string(nil), paths...)
		o.keyPathsSet = true
	}
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home directory: %w", err)
	}
	return filepath.Join(home, ".config", "sshq", "credentials.age"), nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Set(alias, password string) error {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return fmt.Errorf("credential alias required")
	}

	doc, err := s.read()
	if err != nil {
		return err
	}
	doc.Credentials[alias] = password
	return s.write(doc)
}

func (s *Store) Get(alias string) (string, error) {
	doc, err := s.read()
	if err != nil {
		return "", err
	}
	password, ok := doc.Credentials[alias]
	if !ok {
		return "", ErrNotFound
	}
	return password, nil
}

func (s *Store) Delete(alias string) error {
	doc, err := s.read()
	if err != nil {
		return err
	}
	if _, ok := doc.Credentials[alias]; !ok {
		return nil
	}
	delete(doc.Credentials, alias)
	return s.write(doc)
}

func (s *Store) List() ([]string, error) {
	doc, err := s.read()
	if err != nil {
		return nil, err
	}
	aliases := make([]string, 0, len(doc.Credentials))
	for alias := range doc.Credentials {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return aliases, nil
}

func (s *Store) discoverSSHKeys() {
	for _, path := range s.keyPaths {
		identity, recipient, err := s.loadSSHKey(path)
		if err != nil {
			continue
		}
		s.identities = append(s.identities, identity)
		if recipient != nil {
			s.recipients = append(s.recipients, recipient)
		}
	}
}

func (s *Store) loadSSHKey(path string) (age.Identity, age.Recipient, error) {
	privateKey, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil, nil, err
	}

	identity, err := agessh.ParseIdentity(privateKey)
	if err != nil {
		return nil, nil, err
	}

	return identity, recipientFromIdentity(identity, path), nil
}

func recipientFromIdentity(identity age.Identity, keyPath string) age.Recipient {
	switch id := identity.(type) {
	case *agessh.Ed25519Identity:
		return id.Recipient()
	case *agessh.RSAIdentity:
		return id.Recipient()
	}

	recipient, err := readSSHRecipient(keyPath + ".pub")
	if err == nil {
		return recipient
	}
	return nil
}

func readSSHRecipient(path string) (age.Recipient, error) {
	raw, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil, err
	}
	return agessh.ParseRecipient(strings.TrimSpace(string(raw)))
}

func (s *Store) read() (credentialFile, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyCredentialFile(), nil
	}
	if err != nil {
		return credentialFile{}, fmt.Errorf("read credential file: %w", err)
	}
	if err := checkFilePermission(s.path); err != nil {
		return credentialFile{}, err
	}

	plain, err := s.decrypt(raw)
	if err != nil {
		return credentialFile{}, err
	}

	var doc credentialFile
	if err := json.Unmarshal(plain, &doc); err != nil {
		return credentialFile{}, ErrCorrupt
	}
	if doc.Version != fileVersion || doc.Credentials == nil {
		return credentialFile{}, ErrCorrupt
	}
	return doc, nil
}

func (s *Store) write(doc credentialFile) error {
	recipients, err := s.encryptRecipients()
	if err != nil {
		return err
	}

	plain, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal credential file: %w", err)
	}

	var encrypted bytes.Buffer
	w, err := age.Encrypt(&encrypted, recipients...)
	if err != nil {
		return fmt.Errorf("encrypt credentials: %w", err)
	}
	if _, err := w.Write(plain); err != nil {
		w.Close()
		return fmt.Errorf("encrypt credentials: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("encrypt credentials: %w", err)
	}

	return writeFileAtomic(s.path, encrypted.Bytes())
}

func (s *Store) decrypt(raw []byte) ([]byte, error) {
	identities, err := s.decryptIdentities()
	if err != nil {
		return nil, err
	}

	r, err := age.Decrypt(bytes.NewReader(raw), identities...)
	if err != nil {
		var noMatch *age.NoIdentityMatchError
		if errors.As(err, &noMatch) {
			return nil, ErrCannotDecrypt
		}
		return nil, ErrCorrupt
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return nil, ErrCorrupt
	}
	return plain, nil
}

func (s *Store) encryptRecipients() ([]age.Recipient, error) {
	if len(s.recipients) > 0 {
		return append([]age.Recipient(nil), s.recipients...), nil
	}
	if s.passphrase == nil {
		return nil, ErrNoEncryptionKey
	}
	passphrase, err := s.getPassphrase()
	if err != nil {
		return nil, err
	}
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("create passphrase recipient: %w", err)
	}
	return []age.Recipient{recipient}, nil
}

func (s *Store) decryptIdentities() ([]age.Identity, error) {
	identities := append([]age.Identity(nil), s.identities...)
	if s.passphrase != nil {
		passphrase, err := s.getPassphrase()
		if err != nil {
			return nil, err
		}
		identity, err := age.NewScryptIdentity(passphrase)
		if err != nil {
			return nil, fmt.Errorf("create passphrase identity: %w", err)
		}
		identities = append(identities, identity)
	}
	if len(identities) == 0 {
		return nil, ErrCannotDecrypt
	}
	return identities, nil
}

func (s *Store) getPassphrase() (string, error) {
	s.passOnce.Do(func() {
		s.passValue, s.passErr = s.passphrase()
	})
	return s.passValue, s.passErr
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create credential directory: %w", err)
	}
	if err := setDirPermission(dir, 0700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp credential file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpPath)
		}
	}()

	if err := setFilePermission(tmpPath, 0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp credential file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp credential file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp credential file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename credential file: %w", err)
	}
	cleanup = false

	if err := syncDir(dir); err != nil {
		return err
	}
	return nil
}

func emptyCredentialFile() credentialFile {
	return credentialFile{
		Version:     fileVersion,
		Credentials: make(map[string]string),
	}
}

func defaultKeyPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_rsa"),
	}
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
