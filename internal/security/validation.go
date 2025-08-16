package security

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// InputValidator provides comprehensive input validation for security
type InputValidator struct {
	maxLength      int
	allowedChars   *regexp.Regexp
	blockedPatterns []string
}

// NewInputValidator creates a new input validator with security rules
func NewInputValidator() *InputValidator {
	return &InputValidator{
		maxLength:    1000, // Reasonable default
		allowedChars: regexp.MustCompile(`^[a-zA-Z0-9\-_./\s]+$`),
		blockedPatterns: []string{
			"<script", "javascript:", "data:", "vbscript:",
			"../", "..\\", "/etc/", "c:\\", "cmd.exe",
			"SELECT", "INSERT", "UPDATE", "DELETE", "DROP",
			"eval(", "exec(", "system(", "shell(",
		},
	}
}

// ValidateString performs comprehensive string validation
func (v *InputValidator) ValidateString(input string, fieldName string) error {
	if input == "" {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}

	// Length check
	if len(input) > v.maxLength {
		return fmt.Errorf("%s exceeds maximum length of %d characters", fieldName, v.maxLength)
	}

	// Character validation
	if !v.allowedChars.MatchString(input) {
		return fmt.Errorf("%s contains invalid characters", fieldName)
	}

	// Blocked patterns
	inputLower := strings.ToLower(input)
	for _, pattern := range v.blockedPatterns {
		if strings.Contains(inputLower, strings.ToLower(pattern)) {
			return fmt.Errorf("%s contains blocked pattern: %s", fieldName, pattern)
		}
	}

	// Unicode normalization check
	if containsControlChars(input) {
		return fmt.Errorf("%s contains control characters", fieldName)
	}

	return nil
}

// ValidateArtifactHash validates cache artifact hashes
func (v *InputValidator) ValidateArtifactHash(hash string) error {
	if hash == "" {
		return fmt.Errorf("artifact hash cannot be empty")
	}

	// Hash format validation (SHA256 expected)
	hashRegex := regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
	if !hashRegex.MatchString(hash) {
		return fmt.Errorf("invalid hash format, expected SHA256")
	}

	return nil
}

// ValidateInstanceName validates Bazel instance names
func (v *InputValidator) ValidateInstanceName(instanceName string) error {
	if instanceName == "" {
		return fmt.Errorf("instance name cannot be empty")
	}

	// Instance name format validation
	instanceRegex := regexp.MustCompile(`^[a-zA-Z0-9\-_/]+$`)
	if !instanceRegex.MatchString(instanceName) {
		return fmt.Errorf("invalid instance name format")
	}

	// Prevent directory traversal
	if strings.Contains(instanceName, "..") {
		return fmt.Errorf("instance name contains directory traversal")
	}

	return nil
}

// ValidateBuildTarget validates Bazel build targets
func (v *InputValidator) ValidateBuildTarget(target string) error {
	if target == "" {
		return fmt.Errorf("build target cannot be empty")
	}

	// Bazel target format validation
	targetRegex := regexp.MustCompile(`^//[a-zA-Z0-9\-_/.]*:[a-zA-Z0-9\-_]*$`)
	if !targetRegex.MatchString(target) {
		return fmt.Errorf("invalid Bazel target format")
	}

	return nil
}

// ValidateContentType validates HTTP content types
func (v *InputValidator) ValidateContentType(contentType string) error {
	if contentType == "" {
		return nil // Optional field
	}

	// Content type format validation
	contentTypeRegex := regexp.MustCompile(`^[a-zA-Z0-9\-_/+.]+$`)
	if !contentTypeRegex.MatchString(contentType) {
		return fmt.Errorf("invalid content type format")
	}

	// Maximum length for content type
	if len(contentType) > 100 {
		return fmt.Errorf("content type too long")
	}

	return nil
}

// ValidateRedisKey validates Redis key names for security
func (v *InputValidator) ValidateRedisKey(key string) error {
	if key == "" {
		return fmt.Errorf("Redis key cannot be empty")
	}

	// Redis key format validation
	keyRegex := regexp.MustCompile(`^[a-zA-Z0-9\-_:/.]+$`)
	if !keyRegex.MatchString(key) {
		return fmt.Errorf("invalid Redis key format")
	}

	// Prevent Redis command injection
	blockedRedisPatterns := []string{
		"FLUSHALL", "FLUSHDB", "EVAL", "SCRIPT", "DEBUG",
		"CONFIG", "SHUTDOWN", "CLIENT", "MONITOR",
	}

	keyUpper := strings.ToUpper(key)
	for _, pattern := range blockedRedisPatterns {
		if strings.Contains(keyUpper, pattern) {
			return fmt.Errorf("Redis key contains blocked command: %s", pattern)
		}
	}

	return nil
}

// ValidateFilePath validates file paths for security
func (v *InputValidator) ValidateFilePath(path string) error {
	if path == "" {
		return fmt.Errorf("file path cannot be empty")
	}

	// Prevent directory traversal
	if strings.Contains(path, "..") {
		return fmt.Errorf("file path contains directory traversal")
	}

	// Prevent access to sensitive directories
	sensitivePaths := []string{
		"/etc/", "/proc/", "/sys/", "/dev/",
		"c:\\windows", "c:\\program files",
	}

	pathLower := strings.ToLower(path)
	for _, sensitive := range sensitivePaths {
		if strings.HasPrefix(pathLower, sensitive) {
			return fmt.Errorf("file path accesses sensitive directory")
		}
	}

	// File path length check
	if len(path) > 500 {
		return fmt.Errorf("file path too long")
	}

	return nil
}

// containsControlChars checks for dangerous Unicode control characters
func containsControlChars(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return true
		}
	}
	return false
}

// SanitizeForLogging sanitizes strings for safe logging
func SanitizeForLogging(input string) string {
	// Replace potential log injection characters
	sanitized := strings.ReplaceAll(input, "\n", "\\n")
	sanitized = strings.ReplaceAll(sanitized, "\r", "\\r")
	sanitized = strings.ReplaceAll(sanitized, "\t", "\\t")
	
	// Truncate very long strings for logging
	if len(sanitized) > 200 {
		sanitized = sanitized[:200] + "...[truncated]"
	}
	
	return sanitized
}
