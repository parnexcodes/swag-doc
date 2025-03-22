package logger

import (
	"fmt"
	"time"

	"github.com/fatih/color"
)

var (
	// Predefined colors for different log levels
	infoColor     = color.New(color.FgCyan)
	successColor  = color.New(color.FgGreen)
	warningColor  = color.New(color.FgYellow)
	errorColor    = color.New(color.FgRed)
	criticalColor = color.New(color.FgMagenta)

	// Special color combinations for highlights
	highlightSuccess = color.New(color.FgBlack, color.BgGreen)
	highlightError   = color.New(color.FgBlack, color.BgRed)
	highlightWarning = color.New(color.FgBlack, color.BgYellow)

	// Status code colors
	status2xxColor = color.New(color.FgGreen, color.Bold)
	status3xxColor = color.New(color.FgYellow, color.Bold)
	status4xxColor = color.New(color.FgRed, color.Bold)
	status5xxColor = color.New(color.FgMagenta, color.Bold)
)

// PrintInfo prints an info message
func PrintInfo(format string, args ...interface{}) {
	printWithPrefix(infoColor, "INFO", format, args...)
}

// PrintSuccess prints a success message
func PrintSuccess(format string, args ...interface{}) {
	printWithPrefix(successColor, "SUCCESS", format, args...)
}

// PrintWarning prints a warning message
func PrintWarning(format string, args ...interface{}) {
	printWithPrefix(warningColor, "WARNING", format, args...)
}

// PrintError prints an error message
func PrintError(format string, args ...interface{}) {
	printWithPrefix(errorColor, "ERROR", format, args...)
}

// PrintCritical prints a critical error message
func PrintCritical(format string, args ...interface{}) {
	printWithPrefix(criticalColor, "CRITICAL", format, args...)
}

// PrintRequestLog prints a formatted API request/response log
func PrintRequestLog(method, path string, statusCode int, contentType string) {
	timestamp := time.Now().Format("15:04:05")
	timeColor := color.New(color.FgWhite)
	methodColor := color.New(color.FgBlue, color.Bold)

	// Choose appropriate color for status code
	var statusColorFunc *color.Color
	switch {
	case statusCode >= 200 && statusCode < 300:
		statusColorFunc = status2xxColor
	case statusCode >= 300 && statusCode < 400:
		statusColorFunc = status3xxColor
	case statusCode >= 400 && statusCode < 500:
		statusColorFunc = status4xxColor
	default:
		statusColorFunc = status5xxColor
	}

	// Format the log
	fmt.Printf("[%s] ", timeColor.Sprint(timestamp))
	methodColor.Printf("%-6s", method)
	fmt.Printf(" %-40s → ", path)
	statusColorFunc.Printf("%d", statusCode)
	fmt.Printf(" (%s)\n", contentType)
}

// PrintStartupBanner prints a startup banner for the application
func PrintStartupBanner(port int, target, dataDir string) {
	banner := `
   _____                    _____            
  / ____|                  |  __ \           
 | (_____      ____ _  __ _| |  | | ___   ___ 
  \___ \ \ /\ / / _' |/ _' | |  | |/ _ \ / __|
  ____) \ V  V / (_| | (_| | |__| | (_) | (__ 
 |_____/ \_/\_/ \__,_|\__, |_____/ \___/ \___|
                       __/ |                  
                      |___/                   
`
	fmt.Println(color.New(color.FgCyan).Sprint(banner))

	fmt.Printf("%s %s\n",
		color.New(color.FgGreen, color.Bold).Sprint("✓ Proxy Server:"),
		fmt.Sprintf("http://localhost:%d → %s", port, target))

	fmt.Printf("%s %s\n",
		color.New(color.FgGreen, color.Bold).Sprint("✓ Data Directory:"),
		dataDir)

	highlightSuccess.Println(" READY TO CAPTURE API TRAFFIC ")
	fmt.Println(color.New(color.FgWhite).Sprint("------------------------------------------"))
}

// Helper function to print with a prefix
func printWithPrefix(colorFunc *color.Color, prefix, format string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05")
	timeColor := color.New(color.FgWhite)

	fmt.Printf("[%s] ", timeColor.Sprint(timestamp))
	colorFunc.Printf("[%s] ", prefix)
	fmt.Printf(format+"\n", args...)
}

// HighlightHeader returns a highlighted header string suitable for section titles
func HighlightHeader(text string) string {
	return color.New(color.FgBlack, color.BgCyan, color.Bold).Sprint(text)
}
