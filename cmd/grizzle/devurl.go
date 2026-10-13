package main

import "strings"

// parseDevURL extracts the --dev-url flag value from CLI args, supporting both
// --dev-url=value and --dev-url value syntax.
func parseDevURL(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--dev-url=") {
			return strings.TrimPrefix(arg, "--dev-url="), nil
		}
		if arg == "--dev-url" && i+1 < len(args) {
			return args[i+1], nil
		}
	}
	return "", nil
}
