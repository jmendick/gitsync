package util

import (
	"fmt"
	"log"
	"os"
)

// LogError logs an error message to stderr.
func LogError(format string, args ...interface{}) {
	log.Printf("[ERROR] "+format+"\n", args...)
}

// LogInfo logs an informational message to stdout.
func LogInfo(format string, args ...interface{}) {
	fmt.Printf("[INFO] "+format+"\n", args...)
}

// CheckError panics if an error is not nil. (For initial development - replace with proper error handling later)
func CheckError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error: %s\n", err.Error())
		panic(err) // Or os.Exit(1) for less drastic exit
	}
}

// ... (Add more utility functions like string helpers, time formatting, etc.) ...