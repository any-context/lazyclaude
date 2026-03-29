package plugin

import "encoding/json"

// InstalledPlugin represents a plugin from `claude plugins list --json`.
type InstalledPlugin struct {
	ID          string `json:"id"`          // e.g. "lua-lsp@claude-plugins-official"
	Version     string `json:"version"`
	Scope       string `json:"scope"`       // "user", "project", "local"
	Enabled     bool   `json:"enabled"`
	InstallPath string `json:"installPath"`
	InstalledAt string `json:"installedAt"` // ISO 8601
	LastUpdated string `json:"lastUpdated"` // ISO 8601
}

// AvailablePlugin represents a marketplace plugin from `claude plugins list --available --json`.
type AvailablePlugin struct {
	PluginID        string `json:"pluginId"`        // e.g. "code-review@claude-plugins-official"
	Name            string `json:"name"`
	Description     string `json:"description"`
	MarketplaceName string `json:"marketplaceName"`
	Source          Source `json:"source"`
	InstallCount    int    `json:"installCount"`
}

// Source describes the origin of a plugin.
// CLI returns either a struct {"source":"url","url":"..."} or a plain string "./path".
type Source struct {
	Source string `json:"source"` // "github", "url", "path", "npm", "git-subdir"
	Repo   string `json:"repo,omitempty"`
	URL    string `json:"url,omitempty"`
	Ref    string `json:"ref,omitempty"`
	SHA    string `json:"sha,omitempty"`
	Raw    string `json:"-"` // set when source is a plain string (e.g. "./plugins/foo")
}

// UnmarshalJSON handles both string and object forms of source.
func (s *Source) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		s.Source = "path"
		s.Raw = str
		return nil
	}
	type alias Source
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*s = Source(a)
	return nil
}

// ListResult is the output of `claude plugins list --available --json`.
type ListResult struct {
	Installed []InstalledPlugin `json:"installed"`
	Available []AvailablePlugin `json:"available"`
}

// MarketplaceInfo represents a marketplace from `claude plugins marketplace list --json`.
type MarketplaceInfo struct {
	Name            string `json:"name"`
	Source          string `json:"source"` // "github"
	Repo            string `json:"repo"`
	InstallLocation string `json:"installLocation"`
}

// MarketplaceName extracts the marketplace name (after @) from a full plugin ID.
func MarketplaceName(id string) string {
	for i, c := range id {
		if c == '@' {
			return id[i+1:]
		}
	}
	return ""
}
