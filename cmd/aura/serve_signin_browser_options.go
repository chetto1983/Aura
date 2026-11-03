package main

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const signInMinFreeBytes = 8 << 30

type signInListenerAddress struct {
	Host string
	Port int
	Path string
}

func freeBytesFromDf(output []byte) (int64, error) {
	fields := strings.Fields(string(output))
	if len(fields) != 2 {
		return 0, errors.New("could not read free disk space")
	}
	available, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || available < 0 {
		return 0, errors.New("could not read free disk space")
	}
	return available, nil
}

func listenerAddressFromRedirect(raw string) (signInListenerAddress, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		(u.Hostname() != "localhost" && !net.ParseIP(u.Hostname()).IsLoopback()) {
		return signInListenerAddress{}, errors.New("sign-in needs an HTTP loopback callback")
	}
	port := 80
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
	}
	if err != nil || port < 1 || port > 65535 {
		return signInListenerAddress{}, errors.New("invalid sign-in callback port")
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	return signInListenerAddress{Host: u.Hostname(), Port: port, Path: path}, nil
}
