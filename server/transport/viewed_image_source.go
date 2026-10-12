package transport

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ViewedImageBodyPath is the HTTP endpoint for signed viewed-image bodies.
const ViewedImageBodyPath = "/viewed-image"
const viewedImageSourceSignaturePurpose = "kent:viewed-image-original:v1\x00"

type viewedImageSourceDescriptor struct {
	ProjectID       string `json:"project_id"`
	SessionID       string `json:"session_id"`
	Path            string `json:"path"`
	ProtocolVersion string `json:"protocol_version"`
}

func (d viewedImageSourceDescriptor) validate() error {
	if strings.TrimSpace(d.ProjectID) == "" {
		return errors.New("viewed image source Project ID is required")
	}
	if strings.TrimSpace(d.SessionID) == "" {
		return errors.New("viewed image source Session ID is required")
	}
	if strings.TrimSpace(d.Path) == "" {
		return errors.New("viewed image source path is required")
	}
	if strings.TrimSpace(d.ProtocolVersion) == "" {
		return errors.New("viewed image source protocol version is required")
	}
	return nil
}

func (a *sessionReattachAuthority) issueViewedImageSourceURL(
	descriptor viewedImageSourceDescriptor,
) (string, error) {
	if err := descriptor.validate(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(descriptor)
	if err != nil {
		return "", fmt.Errorf("encode viewed image source descriptor: %w", err)
	}
	mac := hmac.New(sha256.New, a.key[:])
	_, _ = mac.Write([]byte(viewedImageSourceSignaturePurpose))
	_, _ = mac.Write(payload)
	query := url.Values{
		"signature": {base64.RawURLEncoding.EncodeToString(mac.Sum(nil))},
		"source":    {base64.RawURLEncoding.EncodeToString(payload)},
	}
	return (&url.URL{Path: ViewedImageBodyPath, RawQuery: query.Encode()}).RequestURI(), nil
}

func (a *sessionReattachAuthority) verifyViewedImageSource(
	encodedSource string,
	encodedSignature string,
) (viewedImageSourceDescriptor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(encodedSource)
	if err != nil {
		return viewedImageSourceDescriptor{}, errors.New("viewed image source descriptor encoding is invalid")
	}
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil {
		return viewedImageSourceDescriptor{}, errors.New("viewed image source signature encoding is invalid")
	}
	if len(signature) != sha256.Size {
		return viewedImageSourceDescriptor{}, errors.New("viewed image source signature length is invalid")
	}
	mac := hmac.New(sha256.New, a.key[:])
	_, _ = mac.Write([]byte(viewedImageSourceSignaturePurpose))
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return viewedImageSourceDescriptor{}, errors.New("viewed image source signature is invalid")
	}
	var descriptor viewedImageSourceDescriptor
	if err := json.Unmarshal(payload, &descriptor); err != nil {
		return viewedImageSourceDescriptor{}, fmt.Errorf("decode viewed image source descriptor: %w", err)
	}
	if err := descriptor.validate(); err != nil {
		return viewedImageSourceDescriptor{}, err
	}
	return descriptor, nil
}
