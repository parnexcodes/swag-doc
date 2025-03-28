package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/parnexcodes/swag-doc/pkg/logger"
	"github.com/parnexcodes/swag-doc/pkg/openapi"
	"github.com/parnexcodes/swag-doc/pkg/proxy"

	"github.com/spf13/cobra"
)

const (
	defaultDataDir = "./swagdoc-data"
	version        = "1.0.0"
)

var (
	// Proxy command flags
	proxyPort    int
	proxyTarget  string
	proxyDataDir string

	// Generate command flags
	generateOutput        string
	generateDataDir       string
	generateTitle         string
	generateDescription   string
	generateVersion       string
	generateBasePath      string
	generateCleanup       bool
	generateUsePathGroups bool
	generateTagMapping    []string
	generateVersionPrefix []string

	// Root command
	rootCmd = &cobra.Command{
		Use:   "swagdoc",
		Short: "Generate Swagger documentation from API transactions",
		Long: `swagdoc is a tool that captures API calls through a proxy and
generates Swagger/OpenAPI documentation based on those transactions.

It works in two steps:
1. Run the proxy server to capture API transactions
2. Generate Swagger/OpenAPI documentation from the captured transactions`,
		Example: `  # Start a proxy server targeting an API
  swagdoc proxy --target http://api.example.com

  # Generate documentation from captured transactions
  swagdoc generate --output swagger.json`,
	}

	// Proxy command
	proxyCmd = &cobra.Command{
		Use:   "proxy",
		Short: "Start a proxy server to capture API transactions",
		Long: `Starts a proxy server that intercepts API requests and responses,
storing them for later use in documentation generation.

NOTE: All captured data is sanitized to remove sensitive information. Only 
data types and structures are preserved, actual values are replaced with
type placeholders to ensure privacy and security.`,
		Example: `  # Start a proxy on the default port 8080
  swagdoc proxy --target http://api.example.com

  # Start a proxy on a custom port with a specific data directory
  swagdoc proxy --target http://api.example.com --port 9000 --data-dir ./api-data`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if proxyTarget == "" {
				return fmt.Errorf("target API server URL is required")
			}
			return runProxy(proxyPort, proxyTarget, proxyDataDir)
		},
	}

	// Generate command
	generateCmd = &cobra.Command{
		Use:   "generate",
		Short: "Generate Swagger documentation from captured API transactions",
		Long: `Generates Swagger/OpenAPI documentation based on previously
captured API transactions from the proxy server.`,
		Example: `  # Generate documentation with default settings
  swagdoc generate

  # Generate documentation with custom settings
  swagdoc generate --output api-docs.json --title "My API" --version "2.0.0"
  
  # Generate documentation and clean up transaction data
  swagdoc generate --output api-docs.json --cleanup
  
  # Generate documentation with custom tag mappings
  swagdoc generate --tag-mapping "auth:Authentication" --tag-mapping "users:User Management"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateDocs(generateOutput, generateDataDir, generateTitle, generateDescription,
				generateVersion, generateBasePath, generateCleanup)
		},
	}

	// Version command
	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print the version number of swagdoc",
		Long:  `All software has versions. This is swagdoc's.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("swagdoc version %s\n", version)
		},
	}

	// Completion command
	completionCmd = &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate completion script for specified shell",
		Long: `To load completions:

Bash:
  $ source <(swagdoc completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ swagdoc completion bash > /etc/bash_completion.d/swagdoc
  # macOS:
  $ swagdoc completion bash > /usr/local/etc/bash_completion.d/swagdoc

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it.  You can execute the following once:

  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ swagdoc completion zsh > "${fpath[1]}/_swagdoc"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ swagdoc completion fish > ~/.config/fish/completions/swagdoc.fish

PowerShell:
  PS> swagdoc completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> swagdoc completion powershell > swagdoc.ps1
  # and source this file from your PowerShell profile.
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.ExactValidArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			switch args[0] {
			case "bash":
				cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			}
		},
	}
)

func init() {
	// Add proxy command flags
	proxyCmd.Flags().IntVarP(&proxyPort, "port", "p", 8080, "Port to run the proxy server on")
	proxyCmd.Flags().StringVarP(&proxyTarget, "target", "t", "", "Target API server URL")
	proxyCmd.Flags().StringVarP(&proxyDataDir, "data-dir", "d", defaultDataDir, "Directory to store API transaction data")
	proxyCmd.MarkFlagRequired("target")

	// Add generate command flags
	generateCmd.Flags().StringVarP(&generateOutput, "output", "o", "swagger.json", "Output file for Swagger documentation")
	generateCmd.Flags().StringVarP(&generateDataDir, "data-dir", "d", defaultDataDir, "Directory to read API transaction data from")
	generateCmd.Flags().StringVar(&generateTitle, "title", "API Documentation", "Title for the API documentation")
	generateCmd.Flags().StringVar(&generateDescription, "description", "Generated API documentation", "Description for the API documentation")
	generateCmd.Flags().StringVarP(&generateVersion, "version", "v", "1.0.0", "API version")
	generateCmd.Flags().StringVar(&generateBasePath, "base-path", "http://localhost:8080", "Base path for the API")
	generateCmd.Flags().BoolVar(&generateCleanup, "cleanup", false, "Delete the data directory after generating documentation")
	generateCmd.Flags().BoolVar(&generateUsePathGroups, "group-by-path", true, "Group API endpoints by path segments")
	generateCmd.Flags().StringSliceVar(&generateTagMapping, "tag-mapping", []string{}, "Custom tag mappings in format 'path:tag' (can be used multiple times)")
	generateCmd.Flags().StringSliceVar(&generateVersionPrefix, "version-prefix", []string{}, "Custom version prefixes (can be used multiple times)")

	// Add commands to root
	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(completionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runProxy(port int, target string, dataDir string) error {
	// Print a beautiful startup banner
	logger.PrintStartupBanner(port, target, dataDir)

	// Print additional info
	logger.PrintInfo("All transactions will be saved to a single consolidated session file")
	logger.PrintInfo("Press Ctrl+C to stop the server")

	// Create storage for API transactions
	storage, err := proxy.NewFileStorage(dataDir)
	if err != nil {
		logger.PrintError("Failed to create storage: %v", err)
		return fmt.Errorf("failed to create storage: %v", err)
	}

	// Create interceptor function
	interceptor := proxy.TransactionInterceptor(storage)

	// Create and start proxy server
	server, err := proxy.NewProxyServer(port, target, interceptor)
	if err != nil {
		logger.PrintError("Failed to create proxy server: %v", err)
		return fmt.Errorf("failed to create proxy server: %v", err)
	}

	if err := server.Start(); err != nil {
		logger.PrintError("Proxy server error: %v", err)
		return fmt.Errorf("proxy server error: %v", err)
	}

	return nil
}

// Function to filter transactions to prioritize successful responses
func prioritizeSuccessfulResponses(transactions []proxy.APITransaction) []proxy.APITransaction {
	// Group transactions by endpoint (method + path)
	endpointMap := make(map[string][]proxy.APITransaction)
	for _, tx := range transactions {
		key := tx.Request.Method + ":" + tx.Request.Path
		endpointMap[key] = append(endpointMap[key], tx)
	}

	// Select the best transaction for each endpoint based on status code
	var filteredTransactions []proxy.APITransaction
	for _, txs := range endpointMap {
		// Sort by status code preference
		best := selectBestTransaction(txs)
		filteredTransactions = append(filteredTransactions, best)
	}

	return filteredTransactions
}

// Function to select the best transaction from a group with the same endpoint
func selectBestTransaction(transactions []proxy.APITransaction) proxy.APITransaction {
	if len(transactions) == 1 {
		return transactions[0]
	}

	// Priority order: 2xx > 3xx > 4xx > 5xx
	var best proxy.APITransaction
	bestPriority := 4 // Default to lowest priority

	for _, tx := range transactions {
		var priority int
		statusCode := tx.Response.StatusCode

		switch {
		case statusCode >= 200 && statusCode < 300:
			priority = 0 // Highest priority
		case statusCode >= 300 && statusCode < 400:
			priority = 1
		case statusCode >= 400 && statusCode < 500:
			priority = 2
		default:
			priority = 3 // Lowest priority
		}

		// Update best if this transaction has higher priority (lower number)
		if priority < bestPriority {
			bestPriority = priority
			best = tx
		} else if priority == bestPriority && statusCode < best.Response.StatusCode {
			// If same priority band, prefer lower status code
			best = tx
		}
	}

	return best
}

// generateDocs generates Swagger/OpenAPI documentation from API transactions
func generateDocs(output string, dataDir string, title string, description string, version string, basePath string, cleanup bool) error {
	// Print header
	fmt.Println(logger.HighlightHeader(" SwagDoc Documentation Generator "))
	logger.PrintInfo("Generating Swagger documentation to %s", output)
	logger.PrintInfo("Reading API transaction data from %s", dataDir)

	// Create absolute path for output file
	absOutput, err := filepath.Abs(output)
	if err != nil {
		logger.PrintError("Failed to get absolute path for output file: %v", err)
		return fmt.Errorf("failed to get absolute path for output file: %v", err)
	}

	// Create storage to read API transactions
	storage, err := proxy.NewFileStorage(dataDir)
	if err != nil {
		logger.PrintError("Failed to create storage: %v", err)
		return fmt.Errorf("failed to create storage: %v", err)
	}

	// Get all transactions
	transactions, err := storage.GetAll()
	if err != nil {
		logger.PrintError("Failed to read API transactions: %v", err)
		return fmt.Errorf("failed to read API transactions: %v", err)
	}

	logger.PrintInfo("Found %d API transactions across all session files", len(transactions))

	// Filter transactions to prioritize successful responses
	filteredTransactions := prioritizeSuccessfulResponses(transactions)

	logger.PrintSuccess("Using %d API transactions after prioritizing successful responses", len(filteredTransactions))

	// Create OpenAPI generator with configuration
	config := openapi.OpenAPIConfig{
		Title:           title,
		Description:     description,
		Version:         version,
		UsePathGroups:   generateUsePathGroups,
		TagMappings:     make(map[string]string),
		VersionPrefixes: make(map[string]bool),
		Servers: []openapi.OpenAPIServer{
			{
				URL:         basePath,
				Description: "API Server",
			},
		},
	}

	// Process tag mappings from command line
	for _, mapping := range generateTagMapping {
		parts := strings.SplitN(mapping, ":", 2)
		if len(parts) == 2 {
			config.TagMappings[parts[0]] = parts[1]
		}
	}

	// Process version prefixes from command line
	for _, prefix := range generateVersionPrefix {
		config.VersionPrefixes[prefix] = true
	}

	// Create generator
	generator := openapi.NewOpenAPIGenerator(config)

	// Add all transactions
	for _, tx := range filteredTransactions {
		generator.AddTransaction(tx)
	}

	// Generate specification
	spec, err := generator.GenerateSpec()
	if err != nil {
		logger.PrintError("Failed to generate specification: %v", err)
		return fmt.Errorf("failed to generate specification: %v", err)
	}

	// Create output directory if needed
	outDir := filepath.Dir(absOutput)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		logger.PrintError("Failed to create output directory: %v", err)
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	// Marshal spec to JSON
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		logger.PrintError("Failed to marshal specification: %v", err)
		return fmt.Errorf("failed to marshal specification: %v", err)
	}

	// Write to output file
	if err := os.WriteFile(absOutput, data, 0644); err != nil {
		logger.PrintError("Failed to write specification to file: %v", err)
		return fmt.Errorf("failed to write specification to file: %v", err)
	}

	logger.PrintSuccess("Swagger documentation generated successfully: %s", absOutput)

	// Clean up data directory if requested
	if cleanup {
		logger.PrintInfo("Cleaning up data directory: %s", dataDir)
		if err := os.RemoveAll(dataDir); err != nil {
			logger.PrintError("Failed to clean up data directory: %v", err)
			return fmt.Errorf("failed to clean up data directory: %v", err)
		}
		logger.PrintSuccess("Data directory cleaned up successfully")
	}

	return nil
}
