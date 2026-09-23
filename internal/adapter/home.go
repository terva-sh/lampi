package adapter

import "fmt"

// HomeDir is the user's home directory. HOME wins, then USERPROFILE.
// Harnesses whose documented default is a dot directory under home use
// this. terva's own layout stays in internal/discover.
func HomeDir(getenv func(string) string) (string, error) {
	if h := getenv("HOME"); h != "" {
		return h, nil
	}
	if h := getenv("USERPROFILE"); h != "" {
		return h, nil
	}
	return "", fmt.Errorf("adapter: HOME is not set")
}
