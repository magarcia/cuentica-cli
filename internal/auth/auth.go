package auth

import (
	"errors"
	"unicode"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "cuentica-cli"
	keyringAccount = "api-key"
	maxTokenLength = 2048
)

var ErrNotFound = errors.New("no API key found")

type Store interface {
	Get() (string, error)
	Set(string) error
	Delete() error
}

type Keyring struct{}

func (Keyring) Get() (string, error) {
	token, err := keyring.Get(keyringService, keyringAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", errors.New("cannot read API key from OS keyring")
	}
	return token, nil
}

func (Keyring) Set(token string) error {
	if err := Validate(token); err != nil {
		return err
	}
	if err := keyring.Set(keyringService, keyringAccount, token); err != nil {
		return errors.New("cannot save API key to OS keyring")
	}
	return nil
}

func (Keyring) Delete() error {
	err := keyring.Delete(keyringService, keyringAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return errors.New("cannot delete API key from OS keyring")
	}
	return nil
}

func Resolve(getenv func(string) string, store Store) (token string, source string, err error) {
	if token := getenv("CUENTICA_API_KEY"); token != "" {
		if err := Validate(token); err != nil {
			return "", "", err
		}
		return token, "environment", nil
	}

	token, err = store.Get()
	if errors.Is(err, ErrNotFound) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", errors.New("cannot read API key from OS keyring")
	}
	if err := Validate(token); err != nil {
		return "", "", err
	}
	return token, "keyring", nil
}

func Validate(token string) error {
	if token == "" {
		return errors.New("API key is empty")
	}
	if len(token) > maxTokenLength {
		return errors.New("API key exceeds the maximum length of 2048 bytes")
	}
	for _, r := range token {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return errors.New("API key must not contain whitespace or control characters")
		}
	}
	return nil
}
