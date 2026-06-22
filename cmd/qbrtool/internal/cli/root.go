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

package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// Global flags
	token   string
	verbose bool

	// Version info
	version = "0.1.0"
)

var rootCmd = &cobra.Command{
	Use:   "qbrtool",
	Short: "Quarterly Board Report Tool",
	Long: `qbrtool exports and analyzes GitHub Project Board items.

It supports:
- Exporting items to JSON with time period and type filtering
- Including archived items (via search API workaround)
- Analyzing items for CVEs, OSS contributions, and more

Examples:
  # Export Q3-2024 items including archived
  qbrtool export --quarter Q3-2024 --include-archived -f q3-2024.json

  # Analyze exported items
  qbrtool analyze -i q3-2024.json --analysis all`,
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "GitHub personal access token (or set GITHUB_TOKEN env var)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")

	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(analyzeCmd)
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("qbrtool version %s\n", version)
	},
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

// GetToken returns the GitHub token from flag or environment
func GetToken() string {
	if token != "" {
		return token
	}
	return os.Getenv("GITHUB_TOKEN")
}

// IsVerbose returns whether verbose mode is enabled
func IsVerbose() bool {
	return verbose
}

// Log prints a message if verbose mode is enabled
func Log(format string, args ...interface{}) {
	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
	}
}
