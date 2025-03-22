package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
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
				// Need at least 2 paths to detect a pattern
				continue
			}

			d.detectPatternInGroup(pathGroup)
		}

		// Handle common REST patterns for resource IDs
		d.detectCommonRESTPatterns(basePath, paths)
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
	// Detect paths like /users/123, /products/456, etc.
	reIDPath := regexp.MustCompile(`^/` + basePath + `/(\d+)(/.*)?$`)

	for _, path := range paths {
		matches := reIDPath.FindStringSubmatch(path)
		if len(matches) > 1 {
			// Found an ID pattern
			pattern := fmt.Sprintf("/%s/{id}", basePath)
			d.patterns[pattern] = "Resource ID"

			// If there's more after the ID, process it recursively
			if len(matches) > 2 && matches[2] != "" {
				subPath := matches[2]
				if len(subPath) > 0 {
					// Process sub-resources e.g., /users/{id}/posts
					d.AddPath(fmt.Sprintf("/%s/id%s", basePath, subPath))
				}
			}
		}
	}

	// Look for UUID patterns
	reUUID := regexp.MustCompile(`^/` + basePath + `/([a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})(/.*)?$`)
	for _, path := range paths {
		matches := reUUID.FindStringSubmatch(path)
		if len(matches) > 1 {
			pattern := fmt.Sprintf("/%s/{uuid}", basePath)
			d.patterns[pattern] = "Resource UUID"
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
	uuidPattern := regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
	allUUIDs := true
	for v := range uniqueValues {
		if !uuidPattern.MatchString(v) {
			allUUIDs = false
			break
		}
	}

	if allUUIDs {
		return "uuid", "UUID identifier"
	}

	// Check for dates
	datePattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	allDates := true
	for v := range uniqueValues {
		if !datePattern.MatchString(v) {
			allDates = false
			break
		}
	}

	if allDates {
		return "date", "Date in YYYY-MM-DD format"
	}

	// Check for slugs
	slugPattern := regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	allSlugs := true
	for v := range uniqueValues {
		if !slugPattern.MatchString(v) {
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

	basePath := segments[0]

	// Try common patterns first
	if len(segments) >= 2 {
		// Check for numeric IDs
		if _, err := strconv.Atoi(segments[1]); err == nil {
			pattern := fmt.Sprintf("/%s/{id}", basePath)
			if _, exists := d.patterns[pattern]; exists {
				// Replace the numeric segment with {id}
				segments[1] = "{id}"
				// If there are more segments, check if they also match patterns
				if len(segments) > 2 {
					// TODO: Handle deeper patterns recursively
				}
				return "/" + strings.Join(segments, "/")
			}
		}

		// Check for UUIDs
		uuidPattern := regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
		if uuidPattern.MatchString(segments[1]) {
			pattern := fmt.Sprintf("/%s/{uuid}", basePath)
			if _, exists := d.patterns[pattern]; exists {
				segments[1] = "{uuid}"
				return "/" + strings.Join(segments, "/")
			}
		}
	}

	// If no pattern matches, return the original path
	return path
}

// GetPathParameters extracts parameters from a path given its template
func GetPathParameters(path, template string) map[string]string {
	params := make(map[string]string)

	// Extract parameter names from the template
	paramPattern := regexp.MustCompile(`{([^}]+)}`)
	matches := paramPattern.FindAllStringSubmatch(template, -1)

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

		match := paramPattern.FindStringSubmatch(segment)
		if len(match) > 1 {
			paramName := match[1]
			paramValue := pathSegments[i]
			params[paramName] = paramValue
		}
	}

	return params
}
