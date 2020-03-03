package dockerVolumeRbd

import (
	"errors"
	"os/exec"
	"strings"
	"time"
)

var (
	defaultShellTimeout = 2 * 60 * time.Second
)

// sh is a simple os.exec Command tool, returns trimmed string output
func sh(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)

	out, err := cmd.CombinedOutput()
	return strings.Trim(string(out), " \n"), err
}

// ShResult used for channel in timeout
type ShResult struct {
	Output string // STDOUT+STDERR
	Err    error  // go error, not STDERR
}

// shWithTimeout will run the Cmd and wait for the specified duration
func shWithTimeout(howLong time.Duration, name string, args ...string) (string, error) {

	// set up the results channel
	resultsChan := make(chan ShResult, 1)

	// fire up the goroutine for the actual shell command
	go func() {
		out, err := sh(name, args...)
		resultsChan <- ShResult{Output: out, Err: err}
	}()

	select {
	case res := <-resultsChan:
		return res.Output, res.Err
	case <-time.After(howLong):
		return "", errors.New("timeout reached")
	}

	return "", nil
}

// shWithDefaultTimeout will use the defaultShellTimeout so you dont have to pass one
func shWithDefaultTimeout(name string, args ...string) (string, error) {
	return shWithTimeout(defaultShellTimeout, name, args...)
}
