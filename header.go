package hstspreload

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// MaxAgeNotPresent indicates that a HSTSHeader.MaxAge value is invalid.
	MaxAgeNotPresent = (-1)

	// 18 weeks
	hstsMinimumMaxAge = 10886400 // seconds

	tenYears = 86400 * 365 * 10 // seconds
)

// An HSTSHeader stores the semantics of an HSTS header.
//
// Note: Unless all values are known at initialization time, use
// NewHSTSHeader() instead of constructing an HSTSHeader directly.
// This ensures that the MaxAge field is initialized to
// MaxAgeNotPresent.
type HSTSHeader struct {
	// MaxAge == MaxAgeNotPresent indicates that this value is invalid.
	// A valid MaxAge value is a non-negative integer.
	MaxAge            int64
	IncludeSubDomains bool
	Preload           bool
}

// NewHSTSHeader constructs a new header with all directive values un-set.
//
// It is requivalent to:
//
//     HSTSHeader{
//       Preload:           false,
//       IncludeSubDomains: false,
//       MaxAge:            MaxAgeNotPresent,
//     }
func NewHSTSHeader() HSTSHeader {
	return HSTSHeader{
		Preload:           false,
		IncludeSubDomains: false,
		MaxAge:            MaxAgeNotPresent,
	}
}

// Iff Issues has no errors, the output integer is the max-age in seconds.
func parseMaxAge(directive string) (maxAge int64, issues Issues) {
	maxAgeNumericalString := directive[8:]

	// TODO: Use more concise validation code to parse a digit string to a signed int.
	for i, c := range maxAgeNumericalString {
		if i == 0 && c == '0' && len(maxAgeNumericalString) > 1 {
			issues = issues.addWarningf("Syntax warning: max-age value contains a leading 0: `%s`", directive)
		}
		if c < '0' || c > '9' {
			return MaxAgeNotPresent, issues.addErrorf("Syntax error: max-age value contains characters that are not digits: `%s`", directive)
		}
	}

	maxAge, err := strconv.ParseInt(maxAgeNumericalString, 10, 64)

	if err != nil {
		return MaxAgeNotPresent, issues.addErrorf("Syntax error: Could not parse max-age value `%s`.", maxAgeNumericalString)
	}

	if maxAge < 0 {
		return MaxAgeNotPresent, issues.addErrorf("Internal error: unexpected negative integer: `%d`", maxAge)
	}

	return maxAge, issues
}

// ParseHeaderString parses an HSTS header. ParseHeaderString will
// report syntax errors and warnings, but does NOT calculate whether the
// header value is semantically valid. (See PreloadableHeaderString() for
// that.)
//
// To interpret the Issues that are returned, see the list of
// conventions in the documentation for Issues.
func ParseHeaderString(headerString string) (hstsHeader HSTSHeader, issues Issues) {
	hstsHeader = NewHSTSHeader()

	directives := strings.Split(headerString, ";")
	for i, directive := range directives {
		// TODO: this trims more than spaces and tabs (LWS). https://crbug.com/596561#c10
		directives[i] = strings.TrimSpace(directive)
	}

	// If strings.Split() is given whitespace, it still returns an (empty) directive.
	// So we handle this case separately.
	if len(directives) == 1 && directives[0] == "" {
		// Return immediately, because all the extra information is redundant.
		return hstsHeader, issues.addWarningf("Syntax warning: Header is empty.")
	}

	for _, directive := range directives {
		directiveEqualsIgnoringCase := func(s string) bool {
			return strings.ToLower(directive) == strings.ToLower(s)
		}

		directiveHasPrefixIgnoringCase := func(prefix string) bool {
			return strings.HasPrefix(strings.ToLower(directive), strings.ToLower(prefix))
		}

		switch {
		case directiveEqualsIgnoringCase("preload"):
			if hstsHeader.Preload {
				issues = issues.addUniqueWarningf("Syntax warning: Header contains a repeated directive: `preload`")
			} else {
				hstsHeader.Preload = true
			}

		case directiveHasPrefixIgnoringCase("preload"):
			issues = issues.addUniqueWarningf("Syntax warning: Header contains a `preload` directive with extra directives.")

		case directiveEqualsIgnoringCase("includeSubDomains"):
			if hstsHeader.IncludeSubDomains {
				issues = issues.addUniqueWarningf("Syntax warning: Header contains a repeated directive: `includeSubDomains`")
			} else {
				hstsHeader.IncludeSubDomains = true
				if directive != "includeSubDomains" {
					issues = issues.addUniqueWarningf(
						"Syntax warning: Header contains the token `%s`. The recommended capitalization is `includeSubDomains`.",
						directive,
					)
				}
			}

		case directiveHasPrefixIgnoringCase("includeSubDomains"):
			issues = issues.addUniqueWarningf("Syntax warning: Header contains an `includeSubDomains` directive with extra directives.")

		case directiveHasPrefixIgnoringCase("max-age="):
			maxAge, maxAgeIssues := parseMaxAge(directive)
			issues = combineIssues(issues, maxAgeIssues)

			if len(maxAgeIssues.Errors) > 0 {
				continue
			}

			if hstsHeader.MaxAge == MaxAgeNotPresent {
				hstsHeader.MaxAge = maxAge
			} else {
				issues = issues.addUniqueWarningf("Syntax warning: Header contains a repeated directive: `max-age`")
			}

		case directiveHasPrefixIgnoringCase("max-age"):
			issues = issues.addUniqueErrorf("Syntax error: A max-age directive name is present without an associated value.")

		case directiveEqualsIgnoringCase(""):
			issues = issues.addUniqueWarningf("Syntax warning: Header includes an empty directive or extra semicolon.")

		default:
			issues = issues.addWarningf("Syntax warning: Header contains an unknown directive: `%s`", directive)
		}
	}
	return hstsHeader, issues
}

func preloadableHeaderPreload(hstsHeader HSTSHeader) (issues Issues) {
	if !hstsHeader.Preload {
		issues = issues.addErrorf("Header requirement error: Header must contain the `preload` directive.")
	}

	return issues
}

func preloadableHeaderSubDomains(hstsHeader HSTSHeader) (issues Issues) {
	if !hstsHeader.IncludeSubDomains {
		issues = issues.addErrorf("Header requirement error: Header must contain the `includeSubDomains` directive.")
	}

	return issues
}

func preloadableHeaderMaxAge(hstsHeader HSTSHeader) (issues Issues) {
	switch {
	case hstsHeader.MaxAge == MaxAgeNotPresent:
		issues = issues.addErrorf("Header requirement error: Header must contain a valid `max-age` directive.")

	case hstsHeader.MaxAge < 0:
		issues = issues.addErrorf("Internal error: encountered an HSTSHeader with a negative max-age that does not equal MaxAgeNotPresent: %d", hstsHeader.MaxAge)

	case hstsHeader.MaxAge < hstsMinimumMaxAge:
		errorStr := fmt.Sprintf(
			"Header requirement error: The max-age must be at least 10886400 seconds (== 18 weeks), but the header currently only has max-age=%d.",
			hstsHeader.MaxAge,
		)
		if hstsHeader.MaxAge == 0 {
			errorStr += " If you are trying to remove this domain from the preload list, please contact Lucas Garron at hstspreload@chromium.org"
		}

		issues = issues.addErrorf(errorStr)

	case hstsHeader.MaxAge > tenYears:
		issues = issues.addWarningf(
			"Header FYI: The max-age (%d seconds) is longer than 10 years, which is an unusually long value.",
			hstsHeader.MaxAge,
		)

	}

	return issues
}

// PreloadableHeader checks whether hstsHeader satisfies all requirements
// for preloading in Chromium.
//
// To interpret the result, see the list of conventions in the
// documentation for Issues.
//
// Most of the time, you'll probably want to use PreloadableHeaderString() instead.
func PreloadableHeader(hstsHeader HSTSHeader) (issues Issues) {
	issues = combineIssues(issues, preloadableHeaderSubDomains(hstsHeader))
	issues = combineIssues(issues, preloadableHeaderPreload(hstsHeader))
	issues = combineIssues(issues, preloadableHeaderMaxAge(hstsHeader))
	return issues
}

func RemovableHeader(hstsHeader HSTSHeader) (issues Issues) {
	if hstsHeader.Preload {
		issues = issues.addErrorf("Header requirement error: For preload list removal, the header must not contain the `preload` directive.")
	}

	if hstsHeader.MaxAge == MaxAgeNotPresent {
		issues = issues.addErrorf("Header requirement error: Header must contain a valid `max-age` directive.")
	}

	return issues
}

// PreloadableHeaderString is a convenience function that calls
// ParseHeaderString() and then calls on PreloadableHeader() the parsed
// header. It returns all issues from both calls, combined.
//
// To interpret the result, see the list of conventions in the
// documentation for Issues.
func PreloadableHeaderString(headerString string) Issues {
	hstsHeader, issues := ParseHeaderString(headerString)
	return combineIssues(issues, PreloadableHeader(hstsHeader))
}

// RemovableHeaderString is a convenience function that calls
// ParseHeaderString() and then calls on RemovableHeader() the parsed
// header. It returns all errors from ParseHeaderString() and all
// issues from RemovableHeader(). Note that *warnings* from
// ParseHeaderString() are ignored, since domains asking to be removed
// will often have minor errors that shouldn't affect removal. It's
// better to have a cleaner verdict in this case.
//
// To interpret the result, see the list of conventions in the
// documentation for Issues.
func RemovableHeaderString(headerString string) Issues {
	hstsHeader, issues := ParseHeaderString(headerString)
	issues = Issues{
		Errors: issues.Errors,
		// Ignore parse warnings for removal testing.
	}
	return combineIssues(issues, RemovableHeader(hstsHeader))
}
