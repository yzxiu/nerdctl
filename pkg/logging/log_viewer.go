/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package logging

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

// Type alias for functions which write out logs to the provided stdout/stderr Writers.
// Depending on the provided `LogViewOptions.Follow` option, the function may block
// indefinitely until something is sent through the `stopChannel`.
type LogViewerFunc func(lvopts LogViewOptions, stdout, stderr io.Writer, stopChannel chan os.Signal) error

var logViewers = make(map[string]LogViewerFunc)

// Registers a LogViewerFunc for the
func RegisterLogViewer(driverName string, lvfn LogViewerFunc) {
	if v, ok := logViewers[driverName]; ok {
		logrus.Warnf("A LogViewerFunc with name %q has already been registered: %#v, overriding with %#v either way", driverName, v, lvfn)
	}
	logViewers[driverName] = lvfn
}

func init() {
	RegisterLogViewer("json-file", viewLogsJSONFile)
	RegisterLogViewer("journald", viewLogsJournald)
}

// Returns a LogViewerFunc for the provided logging driver name.
func getLogViewer(driverName string) (LogViewerFunc, error) {
	lv, ok := logViewers[driverName]
	if !ok {
		return nil, fmt.Errorf("no log viewer type registered for logging driver %q", driverName)
	}
	return lv, nil
}

// Set of options passable to log viewers.
type LogViewOptions struct {
	// Identifier (ID) of the container and namespace it's in.
	ContainerID string
	Namespace   string

	// Absolute path to the nerdctl datastore's root.
	DatastoreRootPath string

	// Whether or not to follow the output of the container logs.
	Follow bool

	// Whether or not to print timestampts for each line.
	Timestamps bool

	// Uint representing the number of most recent log entries to display. 0 = "all".
	Tail uint

	// Start/end timestampts to filter logs by.
	Since string
	Until string
}

func (lvo *LogViewOptions) Validate() error {
	if lvo.ContainerID == "" || lvo.Namespace == "" {
		return fmt.Errorf("log viewing options require a ContainerID and Namespace: %#v", lvo)
	}

	if lvo.DatastoreRootPath == "" || !filepath.IsAbs(lvo.DatastoreRootPath) {
		abs, err := filepath.Abs(lvo.DatastoreRootPath)
		if err != nil {
			return err
		}
		logrus.Warnf("given relative datastore path %q, transformed it to absolute path: %q", lvo.DatastoreRootPath, abs)
		lvo.DatastoreRootPath = abs
	}

	return nil
}

// Implements functionality for loading the logging configuration and
// fetching/outputting container logs based on its internal LogViewOptions.
type ContainerLogViewer struct {
	// Logging configuration.
	loggingConfig LogConfig

	// Log viewing options and filters.
	logViewingOptions LogViewOptions

	// Channel to send stop events to the viewer.
	stopChannel chan os.Signal
}

// Validates the given LogViewOptions, loads the logging config for the
// given container and returns a ContainerLogViewer.
func InitContainerLogViewer(lvopts LogViewOptions, stopChannel chan os.Signal) (*ContainerLogViewer, error) {
	if err := lvopts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid LogViewOptions provided (%#v): %s", lvopts, err)
	}

	lcfg, err := LoadLogConfig(lvopts.DatastoreRootPath, lvopts.Namespace, lvopts.ContainerID)
	if err != nil {
		return nil, fmt.Errorf("failed to load logging config: %s", err)
	}
	lv := &ContainerLogViewer{
		loggingConfig:     lcfg,
		logViewingOptions: lvopts,
		stopChannel:       stopChannel,
	}

	return lv, nil
}

// Prints all logs for this LogViewer's containers to the provided io.Writers.
func (lv *ContainerLogViewer) PrintLogsTo(stdout, stderr io.Writer) error {
	viewerFunc, err := getLogViewer(lv.loggingConfig.Driver)
	if err != nil {
		return err
	}

	return viewerFunc(lv.logViewingOptions, stdout, stderr, lv.stopChannel)
}

// Convenience wrapper for exec.LookPath.
func checkExecutableAvailableInPath(executable string) bool {
	_, err := exec.LookPath(executable)
	return err == nil
}
