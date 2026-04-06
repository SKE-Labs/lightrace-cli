package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

var stdinScanner = bufio.NewScanner(os.Stdin)

func ReadLine(label, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("  %s: ", label)
	}

	if !stdinScanner.Scan() {
		if err := stdinScanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("no input")
	}
	val := strings.TrimSpace(stdinScanner.Text())
	if val == "" {
		return defaultVal, nil
	}
	return val, nil
}

func IsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
