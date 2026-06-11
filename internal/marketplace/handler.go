package marketplace

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Skill represents an agent skill from agentpedia.
type Skill struct {
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	URL         string   `json:"url"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type SkillsResponse struct {
	Skills     []Skill            `json:"skills"`
	Categories map[string]string `json:"categories"` // slug → display name
}

// cache holds the parsed sitemap to avoid refetching on every request.
var (
	cache      SkillsResponse
	cacheMu    sync.RWMutex
	cacheTime  time.Time
	cacheTTL   = 15 * time.Minute
)

// categoryDisplayNames maps sitemap URL slugs to human-readable names.
var categoryDisplayNames = map[string]string{
	"advanced":     "Advanced Patterns",
	"ai-tools":     "AI Tools & Agents",
	"architecture": "Architecture & Design Patterns",
	"async":        "Eliminating Waterfalls",
	"backend":      "Backend Development",
	"bundle":       "Bundle Size Optimization",
	"client":       "Client-Side Data Fetching",
	"communication": "Communication & Collaboration",
	"creative":     "Creative & Visual",
	"database":     "Database & SQL",
	"debugging":    "Debugging & Troubleshooting",
	"devops":       "DevOps & CI/CD",
	"documentation": "Documentation & Writing",
	"documents":    "Documents",
	"expo":         "Expo & Mobile",
	"git":          "Git & Version Control",
	"js":           "JavaScript Performance",
	"marketing":    "Marketing & Growth",
	"mobile":       "Mobile Development",
	"nextjs":       "Next.js",
	"nuxt":         "Nuxt.js",
	"other":        "Other Skills",
	"payments":     "Payments & Billing",
	"python":       "Python Development",
	"react":        "React & React Native",
	"rerender":     "Re-render Optimization",
	"rendering":    "Rendering Performance",
	"security":     "Security & Vulnerability Analysis",
	"seo":          "SEO",
	"server":       "Server-Side Performance",
	"testing":      "Testing & Quality Assurance",
	"typescript":   "TypeScript",
	"ui-design":    "UI/UX Design",
	"vue":          "Vue.js",
	"workflow":     "Workflow & Productivity",
}

// HandleSkills handles GET /api/marketplace/skills.
func HandleSkills(w http.ResponseWriter, r *http.Request) {
	cacheMu.RLock()
	valid := !cacheTime.IsZero() && time.Since(cacheTime) < cacheTTL
	resp := cache
	cacheMu.RUnlock()

	if !valid {
		var err error
		resp, err = fetchAndParseSitemap()
		if err != nil {
			// Return stale cache if available, otherwise error.
			if len(resp.Skills) == 0 {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			// Serve stale cache
		} else {
			cacheMu.Lock()
			cache = resp
			cacheTime = time.Now()
			cacheMu.Unlock()
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleSkill handles GET /api/marketplace/skill?url=...
func HandleSkill(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing url"})
		return
	}

	// Ensure it's an agentpedia URL
	if !strings.Contains(url, "agentpedia.codes/agent-skills/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid url"})
		return
	}

	content, err := fetchSkillContent(url)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(content))
}

func fetchAndParseSitemap() (SkillsResponse, error) {
	resp, err := http.Get("https://agentpedia.codes/agent-skills/sitemap.xml")
	if err != nil {
		return SkillsResponse{}, fmt.Errorf("fetch sitemap: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return SkillsResponse{}, fmt.Errorf("read sitemap: %w", err)
	}

	var result SkillsResponse
	result.Categories = categoryDisplayNames

	// Parse XML sitemap — extract <loc> URLs, filter skill pages.
	// URLs look like: https://agentpedia.codes/agent-skills/{category}/{skill-name}
	re := regexp.MustCompile(`<loc>(https://agentpedia\.codes/agent-skills/([^/]+)/([^<]+))</loc>`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	seen := make(map[string]bool)
	for _, m := range matches {
		fullURL := m[1]
		category := m[2]
		name := strings.TrimSuffix(m[3], "/")

		// Skip category index pages (no skill name after category)
		if !strings.Contains(fullURL, "/agent-skills/"+category+"/") || name == category {
			continue
		}

		key := category + "/" + name
		if seen[key] {
			continue
		}
		seen[key] = true

		title := strings.ReplaceAll(name, "-", " ")
		title = strings.ToUpper(title[:1]) + title[1:]

		result.Skills = append(result.Skills, Skill{
			Name:     name,
			Category: category,
			URL:      fullURL,
			Title:    title,
		})
	}

	return result, nil
}

func fetchSkillContent(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch skill page: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read skill page: %w", err)
	}

	html := string(body)

	// Extract the first markdown code block (the skill content).
	// Skill pages contain the markdown embedded in a code block.
	re := regexp.MustCompile("(?s)<pre[^>]*><code[^>]*>(.*?)</code></pre>")
	match := re.FindStringSubmatch(html)
	if len(match) >= 2 {
		content := match[1]
		// Unescape HTML entities
		content = strings.ReplaceAll(content, "&lt;", "<")
		content = strings.ReplaceAll(content, "&gt;", ">")
		content = strings.ReplaceAll(content, "&amp;", "&")
		content = strings.ReplaceAll(content, "&quot;", "\"")
		content = strings.ReplaceAll(content, "&#39;", "'")
		return content, nil
	}

	return "", fmt.Errorf("could not extract skill content from page")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
