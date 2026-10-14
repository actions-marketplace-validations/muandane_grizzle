package main

import "strings"

// parseDevURL extracts the --dev-url flag value from CLI args, supporting both
// --dev-url=value and --dev-url value syntax.
func parseDevURL(args []string) (string, error) {
	for i := range args {
		arg := args[i]
		if after, ok := strings.CutPrefix(arg, "--dev-url="); ok {
			return after, nil
		}
		if arg == "--dev-url" && i+1 < len(args) {
			return args[i+1], nil
		}
	}
	return "", nil
}
