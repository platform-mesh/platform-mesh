/*
Copyright The Platform Mesh Authors.

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

package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"go.platform-mesh.io/jl/internal/util"
)

const (
	Red   = "\033[31m"
	Green = "\033[32m"
	Blue  = "\033[94m"
	Reset = "\033[0m"
)

var skip string
var focus string
var inputFile string
var raw bool
var showNoJson bool
var spaceLines bool
var noColors bool
var selector []string
var numberOfLines int

func init() {
	rootCmd = &cobra.Command{
		Use:   "jl",
		Short: "jl: is a log display tool to display json based log streams or files beautifully",
		Example: `
# Show kubernetes log streams
kubectl logs my-pod -n my-namespace --follow | jl

# Show logs stored in a file
jl -i data/input.log

# Show logs stored in a file but skip the level and service properties
jl -i data/input.log -s level,service

# Show logs stored in a file but focus only on the message and reconcile_id property
jl -i data/input.log -f message,reconcile_id

# Show logs stored in a file but focus only on the message property, display only rows that match the select expressions
jl -i data/input.log -rf message,level --select=reconcile_id=some-reconcile-id --select=level=info
`,
		Run: ViewLog,
	}

	initialiseFlags()
}
func initialiseFlags() {
	rootCmd.Flags().StringVarP(&skip, "skip", "s", "", "comma separated list of keys to skip")
	rootCmd.Flags().StringVarP(&focus, "focus", "f", "", "comma separated list of keys to put into focus")
	rootCmd.Flags().StringVarP(&inputFile, "input", "i", "", "use file as input")
	rootCmd.Flags().BoolVarP(&raw, "raw", "r", false, "skip json keys")
	rootCmd.Flags().StringSliceVar(&selector, "select", []string{}, "filter logs by selector key=value")
	rootCmd.Flags().BoolVar(&showNoJson, "show-no-json", false, "also display log lines that are no json")
	rootCmd.Flags().BoolVar(&spaceLines, "space-lines", false, "adds an extra line between each log line")
	rootCmd.Flags().BoolVar(&noColors, "no-colors", false, "disable colors")
	rootCmd.Flags().IntVarP(&numberOfLines, "number-of-lines", "n", 0, "number of lines to display")
}

var rootCmd *cobra.Command

func Execute() { // coverage-ignore
	cobra.CheckErr(rootCmd.Execute())
}

func ViewLog(_ *cobra.Command, _ []string) { // coverage-ignore
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println()
	if len(inputFile) > 0 {
		file, err := os.Open(inputFile)
		if err != nil {
			util.PrintErrOut("Error opening file:", err)
			panic(err)
		}
		defer func() {
			if closeErr := file.Close(); closeErr != nil {
				util.PrintErrOut("Error closing file:", closeErr)
			}
		}()
		scanner = bufio.NewScanner(file)
	}
	printedLines := 0
	for scanner.Scan() {
		if numberOfLines > 0 && printedLines >= numberOfLines {
			return
		}
		txt := scanner.Text()
		var result any
		err := json.Unmarshal([]byte(txt), &result)
		if err != nil {
			if showNoJson {
				// Print normal log
				fmt.Println(txt)
				printedLines++
			}
			continue
		}

		data := result.(map[string]any)
		sortedKeys := generateKeys(data)
		printLine(sortedKeys, data)
		printedLines++
	}

	if err := scanner.Err(); err != nil { // coverage-ignore
		util.PrintErrOut("Error reading input:", err)
		os.Exit(1)
	}
}

func printLine(sortedKeys []string, data map[string]any) {
	if len(selector) > 0 {
		for _, s := range selector {
			segments := strings.Split(s, "=")
			if len(segments) != 2 {
				continue
			}
			if data[segments[0]] != segments[1] {
				return
			}
		}
	}

	var line string
	for _, key := range sortedKeys {
		keyColor := Blue
		textColor := Reset
		if key == "message" || key == "msg" || key == "log" {
			textColor = Green
		}
		if key == "level" && data[key] == "error" {
			keyColor = Red
		}

		if val, ok := data[key].(string); ok {
			var keyStr, valStr string
			if noColors {
				keyStr = key
				valStr = val
			} else {
				keyStr = fmt.Sprintf("%s%s%s", keyColor, key, Reset)
				valStr = fmt.Sprintf("%s%v%s", textColor, val, Reset)
			}

			if raw {
				line += fmt.Sprintf("%s ", valStr)
			} else {
				line += fmt.Sprintf("%s: %s, ", keyStr, valStr)
			}
		} else if valB, ok := data[key].(bool); ok {
			var keyStr, valStr string
			if noColors {
				keyStr = key
				valStr = fmt.Sprintf("%t", valB)
			} else {
				keyStr = fmt.Sprintf("%s%s%s", keyColor, key, Reset)
				valStr = fmt.Sprintf("%s%t%s", textColor, valB, Reset)
			}

			if raw {
				line += fmt.Sprintf("%s ", valStr)
			} else {
				line += fmt.Sprintf("%s: %s, ", keyStr, valStr)
			}
		}
	}
	line = strings.Trim(line, ", ")
	fmt.Println(line)
	if spaceLines {
		fmt.Println()
	}
}

func generateKeys(data map[string]any) []string {
	sortedKeys := make([]string, 0, len(data))
	keys := make([]string, 0, len(data))
	toSkip := util.RemoveEmptyStrings(strings.Split(skip, ","))
	toFocus := util.RemoveEmptyStrings(strings.Split(focus, ","))

	if len(toFocus) == 0 {
		for key := range data {
			key = strings.TrimSpace(key)
			if !util.ContainString(toSkip, key) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		sortedKeys = append(sortedKeys, keys...)
	} else {
		sortedKeys = toFocus
	}
	return sortedKeys
}
