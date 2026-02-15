package session

import "strings"

// ParseRepoFlag extracts a trailing --repo owner/repo argument from text.
// Returns the text without the flag and the chosen repo (or defaultRepo).
func ParseRepoFlag(text, defaultRepo string) (string, string) {
	repo := defaultRepo
	prompt := strings.TrimSpace(text)
	if idx := strings.Index(prompt, "--repo "); idx >= 0 {
		parts := strings.Fields(prompt[idx+7:])
		if len(parts) > 0 {
			repo = parts[0]
			prompt = strings.TrimSpace(prompt[:idx])
		}
	}
	return prompt, repo
}
