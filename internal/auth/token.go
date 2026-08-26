package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const (
	tokenAlgorithm = "HS256"
	tokenPartCount = 3
)

type tokenSigner struct {
	secret []byte
	ttl    time.Duration
}

type tokenHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

type tokenClaims struct {
	Subject string `json:"sub"`
	Role    Role   `json:"role"`
	Issued  int64  `json:"iat"`
	Expires int64  `json:"exp"`
}

func (s tokenSigner) sign(identity Identity, now time.Time) (string, error) {
	header, err := json.Marshal(tokenHeader{Algorithm: tokenAlgorithm, Type: "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(tokenClaims{
		Subject: identity.UserID,
		Role:    identity.Role,
		Issued:  now.Unix(),
		Expires: now.Add(s.ttl).Unix(),
	})
	if err != nil {
		return "", err
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claims)
	signingInput := encodedHeader + "." + encodedClaims
	return signingInput + "." + encodeSignature(s.secret, signingInput), nil
}

func (s tokenSigner) verify(token string, now time.Time) (Identity, error) {
	parts := strings.Split(token, ".")
	if len(parts) != tokenPartCount {
		return Identity{}, ErrInvalidToken
	}
	if !hmac.Equal([]byte(parts[2]), []byte(encodeSignature(s.secret, parts[0]+"."+parts[1]))) {
		return Identity{}, ErrInvalidToken
	}

	var header tokenHeader
	if !decodeTokenPart(parts[0], &header) || header.Algorithm != tokenAlgorithm || header.Type != "JWT" {
		return Identity{}, ErrInvalidToken
	}
	var claims tokenClaims
	if !decodeTokenPart(parts[1], &claims) || claims.Subject == "" ||
		(claims.Role != RoleCustomer && claims.Role != RoleAdmin) || claims.Expires <= now.Unix() {
		return Identity{}, ErrInvalidToken
	}
	return Identity{UserID: claims.Subject, Role: claims.Role}, nil
}

func encodeSignature(secret []byte, signingInput string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func decodeTokenPart(encoded string, destination any) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	return err == nil && json.Unmarshal(decoded, destination) == nil
}
