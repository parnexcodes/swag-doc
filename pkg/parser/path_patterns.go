package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Compiled once; previously these were recompiled inside hot functions
// (per path, per segment, per transaction).
var (
	uuidSegmentPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
	dateSegmentPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	slugSegmentPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	paramNamePattern   = regexp.MustCompile(`{([^}]+)}`)
)

// PathPatternDetector detects path patterns from observed URLs
type PathPatternDetector struct {
	pathObservations map[string][]string // base path -> observed paths
	patterns         map[string]string   // detected pattern -> parameter description
}

// NewPathPatternDetector creates a new path pattern detector
func NewPathPatternDetector() *PathPatternDetector {
	return &PathPatternDetector{
		pathObservations: make(map[string][]string),
		patterns:         make(map[string]string),
	}
}

// AddPath adds a path to the detector for analysis
func (d *PathPatternDetector) AddPath(path string) {
	// Split the path into segments
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 {
		return
	}

	// Use the first segment as the base path
	basePath := segments[0]

	// Add the path to the observations
	if _, ok := d.pathObservations[basePath]; !ok {
		d.pathObservations[basePath] = []string{}
	}
	d.pathObservations[basePath] = append(d.pathObservations[basePath], path)
}

// AnalyzePatterns analyzes all observed paths to detect patterns
func (d *PathPatternDetector) AnalyzePatterns() {
	for basePath, paths := range d.pathObservations {
		// Group paths by segment count to compare only paths with the same structure
		pathsBySegmentCount := make(map[int][]string)
		for _, path := range paths {
			segments := strings.Split(strings.Trim(path, "/"), "/")
			pathsBySegmentCount[len(segments)] = append(pathsBySegmentCount[len(segments)], path)
		}

		// Analyze each group
		for _, pathGroup := range pathsBySegmentCount {
			if len(pathGroup) < 2 {
				// Single observation: apply heuristic-based templatization
				if len(pathGroup) == 1 {
					d.templatizeSinglePath(pathGroup[0])
				}
				continue
			}

			d.detectPatternInGroup(pathGroup)
		}

		// Handle common REST patterns for resource IDs
		d.detectCommonRESTPatterns(basePath, paths)
	}
}

// templatizeSinglePath applies heuristic-based templatization for single observations
func (d *PathPatternDetector) templatizeSinglePath(path string) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 2 {
		return
	}

	// Check each segment for potential parameters
	modified := false
	for i, segment := range segments {
		// Numeric ID detection
		if _, err := strconv.Atoi(segment); err == nil {
			// If the previous segment is a known resource noun, templatize
			if i > 0 {
				segments[i] = "{id}"
				modified = true
			}
			continue
		}

		// UUID detection
		if uuidSegmentPattern.MatchString(segment) {
			segments[i] = "{uuid}"
			modified = true
			continue
		}
	}

	if modified {
		pattern := "/" + strings.Join(segments, "/")
		d.patterns[pattern] = "Resource identifier"
	}
}

// detectPatternInGroup detects patterns within a group of paths with the same segment count
func (d *PathPatternDetector) detectPatternInGroup(paths []string) {
	if len(paths) < 2 {
		return
	}

	// Split first path to use as reference
	refPath := paths[0]
	refSegments := strings.Split(strings.Trim(refPath, "/"), "/")

	// Track which segments vary across paths
	varyingSegments := make(map[int][]string)

	// Compare each path to the reference
	for _, path := range paths[1:] {
		segments := strings.Split(strings.Trim(path, "/"), "/")

		// Skip if different segment count (should be handled by caller)
		if len(segments) != len(refSegments) {
			continue
		}

		// Check which segments vary
		for i := 0; i < len(segments); i++ {
			if segments[i] != refSegments[i] {
				varyingSegments[i] = append(varyingSegments[i], segments[i])
				varyingSegments[i] = append(varyingSegments[i], refSegments[i])
			}
		}
	}

	// Create pattern for each varying segment
	for i, values := range varyingSegments {
		// Create a template path
		templateSegments := make([]string, len(refSegments))
		copy(templateSegments, refSegments)

		// Determine parameter type
		paramName, paramDesc := d.inferParameterType(values)

		// Replace varying segment with parameter
		templateSegments[i] = fmt.Sprintf("{%s}", paramName)

		// Create the pattern
		pattern := "/" + strings.Join(templateSegments, "/")
		d.patterns[pattern] = paramDesc
	}
}

// detectCommonRESTPatterns detects common REST patterns
func (d *PathPatternDetector) detectCommonRESTPatterns(basePath string, paths []string) {
	// Detect paths like /users/123, /products/456, etc. — string prefix
	// parsing instead of per-call regex compilation (same semantics).
	prefix := "/" + basePath + "/"

	for _, path := range paths {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(path, prefix)
		idPart := rest
		if slash := strings.IndexByte(rest, '/'); slash != -1 {
			idPart = rest[:slash]
		}
		if idPart == "" {
			continue
		}

		// Found an ID pattern
		if _, err := strconv.Atoi(idPart); err == nil {
			d.patterns[fmt.Sprintf("/%s/{id}", basePath)] = "Resource ID"

			// If there's more after the ID, process it recursively
			if len(rest) > len(idPart) {
				// Process sub-resources e.g., /users/{id}/posts
				d.AddPath(fmt.Sprintf("/%s/id%s", basePath, rest[len(idPart):]))
			}
		}
	}

	// Look for UUID patterns
	for _, path := range paths {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(path, prefix)
		idPart := rest
		if slash := strings.IndexByte(rest, '/'); slash != -1 {
			idPart = rest[:slash]
		}
		if idPart == "" {
			continue
		}

		if uuidSegmentPattern.MatchString(idPart) {
			d.patterns[fmt.Sprintf("/%s/{uuid}", basePath)] = "Resource UUID"
		}
	}
}

// inferParameterType tries to infer the type of parameter from observed values
func (d *PathPatternDetector) inferParameterType(values []string) (string, string) {
	// Remove duplicates
	uniqueValues := make(map[string]struct{})
	for _, v := range values {
		uniqueValues[v] = struct{}{}
	}

	// Empty values should return a generic parameter
	if len(uniqueValues) == 0 {
		return "param", "Path parameter"
	}

	// Check if all values are numeric
	allNumeric := true
	for v := range uniqueValues {
		if _, err := strconv.Atoi(v); err != nil {
			allNumeric = false
			break
		}
	}

	if allNumeric {
		return "id", "Numeric identifier"
	}

	// Check for UUIDs
	allUUIDs := true
	for v := range uniqueValues {
		if !uuidSegmentPattern.MatchString(v) {
			allUUIDs = false
			break
		}
	}

	if allUUIDs {
		return "uuid", "UUID identifier"
	}

	// Check for dates
	allDates := true
	for v := range uniqueValues {
		if !dateSegmentPattern.MatchString(v) {
			allDates = false
			break
		}
	}

	if allDates {
		return "date", "Date in YYYY-MM-DD format"
	}

	// Check for slugs
	allSlugs := true
	for v := range uniqueValues {
		if !slugSegmentPattern.MatchString(v) {
			allSlugs = false
			break
		}
	}

	if allSlugs {
		return "slug", "URL-friendly identifier"
	}

	// Default to generic parameter
	return "param", "Path parameter"
}

// GetPatterns returns all detected patterns
func (d *PathPatternDetector) GetPatterns() map[string]string {
	return d.patterns
}

// TemplatizePath converts a concrete path to a templated path if it matches a pattern
func (d *PathPatternDetector) TemplatizePath(path string) string {
	// Check if this path exactly matches a pattern
	if _, exists := d.patterns[path]; exists {
		return path
	}

	// Try to match with existing patterns
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 {
		return path
	}

	// Try common patterns for all segments
	modified := false
	for i, segment := range segments {
		// Check for numeric IDs (skip first segment — it's likely a resource name)
		if i > 0 {
			if _, err := strconv.Atoi(segment); err == nil {
				segments[i] = "{id}"
				modified = true
				continue
			}
		}

		// Check for UUIDs
		if uuidSegmentPattern.MatchString(segment) {
			segments[i] = "{uuid}"
			modified = true
			continue
		}

		// Check if the segment matches a known pattern
		partialPath := "/" + strings.Join(segments[:i+1], "/")
		if _, exists := d.patterns[partialPath]; exists {
			modified = true
		}
	}

	if modified {
		return "/" + strings.Join(segments, "/")
	}

	// If no pattern matches, return the original path
	return path
}

// GetPathParameters extracts parameters from a path given its template
func GetPathParameters(path, template string) map[string]string {
	params := make(map[string]string)

	// Extract parameter names from the template
	matches := paramNamePattern.FindAllStringSubmatch(template, -1)

	if len(matches) == 0 {
		return params
	}

	// Split both paths
	pathSegments := strings.Split(strings.Trim(path, "/"), "/")
	templateSegments := strings.Split(strings.Trim(template, "/"), "/")

	// Match parameters with values
	for i, segment := range templateSegments {
		if i >= len(pathSegments) {
			break
		}

		match := paramNamePattern.FindStringSubmatch(segment)
		if len(match) > 1 {
			paramName := match[1]
			paramValue := pathSegments[i]
			params[paramName] = paramValue
		}
	}

	return params
}
