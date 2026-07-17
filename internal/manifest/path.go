package manifest

import "strings"

// ExtractEnv pulls the environment segment out of a virtual path such as
// "overlay/dev4/secrets.yaml" -> "dev4".
func ExtractEnv(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "overlay" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
