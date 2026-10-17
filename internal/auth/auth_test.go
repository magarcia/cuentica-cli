package auth

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type fakeStore struct {
	token string
	err   error
	gets  int
}

func (s *fakeStore) Get() (string, error) {
	s.gets++
	return s.token, s.err
}

func (s *fakeStore) Set(token string) error {
	s.token = token
	return s.err
}

func (s *fakeStore) Delete() error {
	s.token = ""
	return s.err
}

func TestResolveEnvironmentTakesPrecedence(t *testing.T) {
	store := &fakeStore{token: "saved-key"}
	token, source, err := Resolve(func(name string) string {
		if name != "CUENTICA_API_KEY" {
			t.Fatalf("unexpected environment variable %q", name)
		}
		return "environment-key"
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if token != "environment-key" || source != "environment" {
		t.Fatalf("unexpected credentials: token=%q source=%q", token, source)
	}
	if store.gets != 0 {
		t.Fatal("environment credentials should avoid accessing the keyring")
	}
}

func TestResolveEmptyEnvironmentUsesKeyring(t *testing.T) {
	store := &fakeStore{token: "saved-key"}
	token, source, err := Resolve(func(string) string { return "" }, store)
	if err != nil {
		t.Fatal(err)
	}
	if token != "saved-key" || source != "keyring" || store.gets != 1 {
		t.Fatalf("unexpected credentials: token=%q source=%q reads=%d", token, source, store.gets)
	}
}

func TestResolveInvalidEnvironmentDoesNotFallBack(t *testing.T) {
	for _, value := range []string{" ", " key", "key ", "key\n", "key\x00"} {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			store := &fakeStore{token: "saved-key"}
			token, source, err := Resolve(func(string) string { return value }, store)
			if err == nil || token != "" || source != "" {
				t.Fatalf("expected validation error, got token=%q source=%q err=%v", token, source, err)
			}
			if store.gets != 0 {
				t.Fatal("invalid environment credentials should not fall back to the keyring")
			}
		})
	}
}

func TestResolveKeyringErrors(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		store := &fakeStore{err: fmt.Errorf("lookup: %w", ErrNotFound)}
		_, _, err := Resolve(func(string) string { return "" }, store)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
	t.Run("sanitized", func(t *testing.T) {
		secret := "secret-that-must-not-be-printed"
		store := &fakeStore{err: errors.New(secret)}
		token, source, err := Resolve(func(string) string { return "" }, store)
		if err == nil || strings.Contains(err.Error(), secret) || token != "" || source != "" {
			t.Fatalf("keyring error was not sanitized: token=%q source=%q err=%v", token, source, err)
		}
	})
}

func TestResolveValidatesSavedCredentials(t *testing.T) {
	for _, value := range []string{"", "saved key", "saved\r\nkey"} {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			store := &fakeStore{token: value}
			token, source, err := Resolve(func(string) string { return "" }, store)
			if err == nil || token != "" || source != "" {
				t.Fatalf("expected validation error, got token=%q source=%q err=%v", token, source, err)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name  string
		token string
		valid bool
	}{
		{name: "ordinary token", token: "abc-123_xyz.456", valid: true},
		{name: "length boundary", token: strings.Repeat("a", maxTokenLength), valid: true},
		{name: "empty"},
		{name: "too long", token: strings.Repeat("a", maxTokenLength+1)},
		{name: "space", token: "secret key"},
		{name: "newline", token: "secret\nkey"},
		{name: "carriage return", token: "secret\rkey"},
		{name: "tab", token: "secret\tkey"},
		{name: "null", token: "secret\x00key"},
		{name: "delete control", token: "secret\x7fkey"},
		{name: "unicode space", token: "secret\u00a0key"},
		{name: "unicode control", token: "secret\u0085key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.token)
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%t, got %v", tt.valid, err)
			}
			if err != nil && tt.token != "" && strings.Contains(err.Error(), tt.token) {
				t.Fatal("validation error must not contain the API key")
			}
		})
	}
}
