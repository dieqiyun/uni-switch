package gui

import (
	"slices"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

func addCodexModelEffort(s *sessions.Session, byThread map[string][]sessions.ModelEffort) {
	if s.Agent != "codex" {
		return
	}
	if s.Role == "" {
		s.Role = "unknown"
	}
	s.ModelEffort = slices.Clone(byThread[s.ID])
}

func addCodexSummaryModelEffort(s *sessions.Summary, byThread map[string][]sessions.ModelEffort) {
	if s.Agent != "codex" {
		return
	}
	if s.Role == "" {
		s.Role = "unknown"
	}
	s.ModelEffort = slices.Clone(byThread[s.ID])
}
