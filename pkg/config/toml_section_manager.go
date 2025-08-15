package config

import (
	"fmt"
	"strings"
)

// TOMLSectionManager manages sections in TOML files
type TOMLSectionManager struct{}

// NewTOMLSectionManager creates a new instance of the section manager
func NewTOMLSectionManager() *TOMLSectionManager {
	return &TOMLSectionManager{}
}

// FindSection finds the start and end of a specific section
// Returns the section start index and the end index (exclusive)
// If the section is not found, returns -1, -1
func (tsm *TOMLSectionManager) FindSection(lines []string, sectionName string) (int, int) {
	sectionHeader := "[" + sectionName + "]"
	start := -1
	end := -1

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Found the section start
		if trimmedLine == sectionHeader {
			start = i
			continue
		}

		// If we already found the section start and found another section, then this is the end
		if start != -1 && strings.HasPrefix(trimmedLine, "[") && !strings.HasPrefix(trimmedLine, "["+sectionName+".") {
			end = i
			break
		}
	}

	// If we found the start but not the end, the end is the end of the file
	if start != -1 && end == -1 {
		end = len(lines)
	}

	return start, end
}

// RemoveSection removes a specific section from the content
func (tsm *TOMLSectionManager) RemoveSection(lines []string, sectionName string) []string {
	start, end := tsm.FindSection(lines, sectionName)

	// If the section was not found, return the original content
	if start == -1 {
		return lines
	}

	// Remove the section
	result := append([]string{}, lines[:start]...)
	if end < len(lines) {
		result = append(result, lines[end:]...)
	}

	return result
}

// RemoveSubSections removes all subsections of a specific section
func (tsm *TOMLSectionManager) RemoveSubSections(lines []string, sectionName string) []string {
	var result []string
	inSubSection := false
	subSectionPrefix := "[" + sectionName + "."

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Check if we are entering a subsection
		if strings.HasPrefix(trimmedLine, subSectionPrefix) {
			inSubSection = true
			continue
		}

		// Check if we are exiting a subsection
		if inSubSection && strings.HasPrefix(trimmedLine, "[") && !strings.HasPrefix(trimmedLine, subSectionPrefix) {
			inSubSection = false
		}

		// Add the line if not in a subsection
		if !inSubSection {
			result = append(result, line)
		}
	}

	return result
}

// AddSection adds a new section to the content
func (tsm *TOMLSectionManager) AddSection(lines []string, sectionName string, sectionContent []string) []string {
	// First, remove any existing section with the same name
	lines = tsm.RemoveSection(lines, sectionName)

	// Add a blank line before the new section if it doesn't end with one
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}

	// Add the section header
	lines = append(lines, "["+sectionName+"]")

	// Add the section content
	if len(sectionContent) > 0 {
		lines = append(lines, sectionContent...)
	}

	return lines
}

// FormatNetworkSection formats the networks section correctly
func (tsm *TOMLSectionManager) FormatNetworkSection(networks map[string]Network) []string {
	var result []string

	// If there are no networks, return an empty list
	if len(networks) == 0 {
		return result
	}

	// Add the [networks] section
	result = append(result, "[networks]")

	// For each network, add a subsection
	for key, network := range networks {
		// Add a blank line to separate subsections
		result = append(result, "")

		// Add the subsection header
		result = append(result, fmt.Sprintf("[networks.%s]", key))

		// Add the network fields
		result = append(result, fmt.Sprintf("name = %q", network.Name))
		result = append(result, fmt.Sprintf("rpc_endpoint = %q", network.RPCEndpoint))
		result = append(result, fmt.Sprintf("chain_id = %d", network.ChainID))
		result = append(result, fmt.Sprintf("symbol = %q", network.Symbol))
		result = append(result, fmt.Sprintf("explorer = %q", network.Explorer))
		result = append(result, fmt.Sprintf("is_active = %t", network.IsActive))
	}

	return result
}

// SanitizeNetworkKey sanitizes a key to ensure it is valid for TOML
func (tsm *TOMLSectionManager) SanitizeNetworkKey(key string) string {
	// Replace invalid characters with underscore
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, key)
}

// GenerateNetworkKey generates a unique and valid key for a network
func (tsm *TOMLSectionManager) GenerateNetworkKey(name string, chainID int64) string {
	// Sanitize the name
	sanitizedName := tsm.SanitizeNetworkKey(name)

	// Create the key in the format custom_{sanitized_name}_{chain_id}
	return fmt.Sprintf("custom_%s_%d", sanitizedName, chainID)
}
