package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

// tunnelOfCredential is the tunnel a stored connector credential runs. The token is the standard
// base64 of a JSON object whose "t" is the tunnel ID (cloudflared connection/connection.go,
// TunnelToken and its Encode; cmd/cloudflared/tunnel/subcommands.go, ParseToken).
func tunnelOfCredential(token string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return "", err
	}
	var fields struct {
		Tunnel string `json:"t"`
	}
	if err = json.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	if fields.Tunnel == "" {
		return "", errors.New("remote access: connector credential names no tunnel")
	}
	return fields.Tunnel, nil
}
