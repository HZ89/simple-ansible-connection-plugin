package utils

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

func GetUserEnvFunc(u *user.User) (func(string) string, error) {

	return func(s string) string {
		switch s {
		case "HOME":
			return u.HomeDir
		case "USER":
			return u.Username
		case "LOGNAME":
			return u.Username
		default:
			v, _ := GetEnv(s)
			return v
		}
	}, nil
}

// GetEnv is a wrapper around os.LookupEnv for easier testing
var GetEnv = func(key string) (string, bool) {
	return os.LookupEnv(key)
}

// ExpandHomeDirectory expands the tilde in the given path based on the provided username.
func ExpandHomeDirectory(username, path string) (string, error) {
	u, err := LookupUser(username)
	if err != nil {
		return "", err
	}

	if strings.HasPrefix(path, "~/") {
		return filepath.Join(u.HomeDir, filepath.Clean(path[2:])), nil
	}
	return path, nil
}

// LookupUser is a wrapper around user.Lookup for easier testing
var LookupUser = func(username string) (*user.User, error) {
	return user.Lookup(username)
}

func GetUserIDs(username string) (int, int, error) {
	usr, err := LookupUser(username)
	if err != nil {
		return 0, 0, fmt.Errorf("user lookup failed: %v", err)
	}

	uid, err := strconv.Atoi(usr.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid UID: %v", err)
	}

	gid, err := strconv.Atoi(usr.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid GID: %v", err)
	}

	return uid, gid, nil
}
