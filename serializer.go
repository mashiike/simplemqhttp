package simplemqhttp

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
)

type Serializer interface {
	// Serialize serializes the given data into a byte slice.
	Serialize(req *http.Request) (string, error)
	// Deserialize deserializes the given byte slice into the specified data structure.
	Deserialize(content string) (*http.Request, error)
}

type BodyOnlySerializer struct {
	NoBase64 bool
}

var ErrTooLarge = errors.New("body too large")

func (s *BodyOnlySerializer) Serialize(req *http.Request) (string, error) {
	if req == nil {
		return "", errors.New("request is nil")
	}
	if req.Body == nil {
		return "", nil
	}
	bs, err := io.ReadAll(req.Body)
	if err != nil {
		return "", err
	}
	req.Body.Close()

	if s.NoBase64 {
		// over 256KB
		if len(bs) > 256*1024 {
			return "", ErrTooLarge
		}
		return string(bs), nil
	}
	encoded := base64.StdEncoding.EncodeToString(bs)
	if len(encoded) > 256*1024 {
		return "", ErrTooLarge
	}
	return encoded, nil
}

func (s *BodyOnlySerializer) Deserialize(content string) (*http.Request, error) {
	if !s.NoBase64 {
		decoded, err := base64.StdEncoding.DecodeString(content)
		if err == nil {
			content = string(decoded)
		}
	}
	req, err := http.NewRequest(http.MethodPost, "/", strings.NewReader(content))
	if err != nil {
		return nil, err
	}
	return req, nil
}
