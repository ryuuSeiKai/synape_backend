package marketplace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleSkill_SSRF_Protection(t *testing.T) {
	tests := []struct {
		name           string
		urlQuery       string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "Valid URL but offline/not found",
			urlQuery:       "https://agentpedia.codes/agent-skills/some-skill",
			expectedStatus: http.StatusInternalServerError, // because fetching offline/empty body in test
			expectedError:  "",
		},
		{
			name:           "Missing URL query parameter",
			urlQuery:       "",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing url",
		},
		{
			name:           "SSRF attempt: evil host with query param",
			urlQuery:       "http://evil.com/agent-skills/?q=agentpedia.codes/agent-skills/",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid url scheme (must be https)",
		},
		{
			name:           "SSRF attempt: https evil host with path",
			urlQuery:       "https://evil.com/agentpedia.codes/agent-skills/",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid url host",
		},
		{
			name:           "SSRF attempt: invalid path",
			urlQuery:       "https://agentpedia.codes/other-path/agent-skills/",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid url path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/api/marketplace/skill?url="+tt.urlQuery, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			rr := httptest.NewRecorder()
			handler := http.HandlerFunc(HandleSkill)
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			if tt.expectedError != "" {
				var resp map[string]string
				if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp["error"] != tt.expectedError {
					t.Errorf("expected error %q, got %q", tt.expectedError, resp["error"])
				}
			}
		})
	}
}
