package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrintInfo(t *testing.T) {
	// Should not panic
	assert.NotPanics(t, func() {
		PrintInfo("test message %s", "arg")
	})
}

func TestPrintSuccess(t *testing.T) {
	assert.NotPanics(t, func() {
		PrintSuccess("success %d", 42)
	})
}

func TestPrintWarning(t *testing.T) {
	assert.NotPanics(t, func() {
		PrintWarning("warning")
	})
}

func TestPrintError(t *testing.T) {
	assert.NotPanics(t, func() {
		PrintError("error: %v", "something went wrong")
	})
}

func TestPrintCritical(t *testing.T) {
	assert.NotPanics(t, func() {
		PrintCritical("critical failure")
	})
}

func TestPrintRequestLog(t *testing.T) {
	assert.NotPanics(t, func() {
		PrintRequestLog("GET", "/api/users", 200, "application/json")
	})
	assert.NotPanics(t, func() {
		PrintRequestLog("POST", "/api/users", 201, "application/json")
	})
	assert.NotPanics(t, func() {
		PrintRequestLog("GET", "/api/users/999", 404, "application/json")
	})
	assert.NotPanics(t, func() {
		PrintRequestLog("GET", "/api/error", 500, "text/html")
	})
}

func TestPrintStartupBanner(t *testing.T) {
	assert.NotPanics(t, func() {
		PrintStartupBanner(8080, "http://localhost:3000", "./data")
	})
}

func TestHighlightHeader(t *testing.T) {
	result := HighlightHeader(" Test Header ")
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "Test Header")
}
