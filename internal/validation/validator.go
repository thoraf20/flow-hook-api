~package validation

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var (
	// Email validation regex
	emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

	// URL validation regex
	urlRegex = regexp.MustCompile(`^https?://[^\s/$.?#].[^\s]*$`)

	// Slug validation (alphanumeric, hyphens, underscores)
	slugRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	// SQL injection patterns
	sqlInjectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(\bunion\b.*\bselect\b)`),
		regexp.MustCompile(`(?i)(\bselect\b.*\bfrom\b)`),
		regexp.MustCompile(`(?i)(\bdrop\b.*\btable\b)`),
		regexp.MustCompile(`(?i)(\bdelete\b.*\bfrom\b)`),
		regexp.MustCompile(`(?i)(\binsert\b.*\binto\b)`),
		regexp.MustCompile(`(?i)(\bupdate\b.*\bset\b)`),
		regexp.MustCompile(`(?i)(\bexec\b|\bexecute\b)`),
		regexp.MustCompile(`(?i)(\bscript\b.*\b>.*<)`),
		regexp.MustCompile(`['";]`),
	}

	// XSS patterns
	xssPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`),
		regexp.MustCompile(`(?i)javascript:`),
		regexp.MustCompile(`(?i)on\w+\s*=`),
		regexp.MustCompile(`(?i)<iframe[^>]*>`),
		regexp.MustCompile(`(?i)<object[^>]*>`),
		regexp.MustCompile(`(?i)<embed[^>]*>`),
	}
)

// ValidateEmail validates an email address
func ValidateEmail(email string) error {
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if len(email) > 255 {
		return fmt.Errorf("email is too long (max 255 characters)")
	}
	if !emailRegex.MatchString(email) {
		return fmt.Errorf("invalid email format")
	}
	return nil
}

// ValidatePassword validates a password
func ValidatePassword(password string) error {
	if password == "" {
		return fmt.Errorf("password is required")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(password) > 128 {
		return fmt.Errorf("password is too long (max 128 characters)")
	}

	var hasUpper, hasLower, hasNumber bool
	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		}
	}

	if !hasUpper || !hasLower || !hasNumber {
		return fmt.Errorf("password must contain at least one uppercase letter, one lowercase letter, and one number")
	}

	return nil
}

// ValidateURL validates a URL
func ValidateURL(urlStr string) error {
	if urlStr == "" {
		return fmt.Errorf("URL is required")
	}
	if len(urlStr) > 2048 {
		return fmt.Errorf("URL is too long (max 2048 characters)")
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL format: %v", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("URL must use http or https scheme")
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("URL must have a host")
	}

	// Check for dangerous patterns
	if containsSQLInjection(urlStr) {
		return fmt.Errorf("URL contains potentially dangerous content")
	}

	return nil
}

// ValidateSlug validates a slug
func ValidateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("slug is required")
	}
	if len(slug) > 100 {
		return fmt.Errorf("slug is too long (max 100 characters)")
	}
	if !slugRegex.MatchString(slug) {
		return fmt.Errorf("slug can only contain alphanumeric characters, hyphens, and underscores")
	}
	return nil
}

// ValidateStringLength validates string length
func ValidateStringLength(s string, min, max int, fieldName string) error {
	if min > 0 && len(s) < min {
		return fmt.Errorf("%s must be at least %d characters", fieldName, min)
	}
	if max > 0 && len(s) > max {
		return fmt.Errorf("%s must be at most %d characters", fieldName, max)
	}
	return nil
}

// SanitizeString removes potentially dangerous characters
func SanitizeString(s string) string {
	// Remove null bytes
	s = strings.ReplaceAll(s, "\x00", "")

	// Remove control characters except newline and tab
	var builder strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

// ContainsSQLInjection checks if a string contains SQL injection patterns
func containsSQLInjection(s string) bool {
	s = strings.ToLower(s)
	for _, pattern := range sqlInjectionPatterns {
		if pattern.MatchString(s) {
			return true
		}
	}
	return false
}

// ContainsXSS checks if a string contains XSS patterns
func ContainsXSS(s string) bool {
	s = strings.ToLower(s)
	for _, pattern := range xssPatterns {
		if pattern.MatchString(s) {
			return true
		}
	}
	return false
}

// SanitizeInput sanitizes user input
func SanitizeInput(input string) string {
	// Remove SQL injection patterns
	if containsSQLInjection(input) {
		// Replace dangerous patterns with safe alternatives
		for _, pattern := range sqlInjectionPatterns {
			input = pattern.ReplaceAllString(input, "")
		}
	}

	// Remove XSS patterns
	if ContainsXSS(input) {
		for _, pattern := range xssPatterns {
			input = pattern.ReplaceAllString(input, "")
		}
	}

	// General sanitization
	input = SanitizeString(input)

	return strings.TrimSpace(input)
}

// ValidateJSON validates that a string is valid JSON
func ValidateJSON(jsonStr string) error {
	if jsonStr == "" {
		return nil // Empty string is valid (optional field)
	}

	// Basic JSON structure check
	jsonStr = strings.TrimSpace(jsonStr)
	if !strings.HasPrefix(jsonStr, "{") && !strings.HasPrefix(jsonStr, "[") {
		return fmt.Errorf("invalid JSON format")
	}

	// Check for balanced braces/brackets
	openBraces := 0
	openBrackets := 0
	inString := false
	escapeNext := false

	for _, char := range jsonStr {
		if escapeNext {
			escapeNext = false
			continue
		}

		switch char {
		case '\\':
			escapeNext = true
		case '"':
			inString = !inString
		case '{':
			if !inString {
				openBraces++
			}
		case '}':
			if !inString {
				openBraces--
			}
		case '[':
			if !inString {
				openBrackets++
			}
		case ']':
			if !inString {
				openBrackets--
			}
		}
	}

	if openBraces != 0 || openBrackets != 0 {
		return fmt.Errorf("invalid JSON: unbalanced braces or brackets")
	}

	return nil
}

// ValidateRateLimit validates rate limit values
func ValidateRateLimit(limit int) error {
	if limit < 0 {
		return fmt.Errorf("rate limit cannot be negative")
	}
	if limit > 1000000 {
		return fmt.Errorf("rate limit is too high (max 1,000,000)")
	}
	return nil
}

// ValidateRetentionDays validates retention days
func ValidateRetentionDays(days int) error {
	if days < 0 {
		return fmt.Errorf("retention days cannot be negative")
	}
	if days > 3650 { // 10 years max
		return fmt.Errorf("retention days is too high (max 3650 days)")
	}
	return nil
}

