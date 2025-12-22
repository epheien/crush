package models

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/tui/exp/list"
	"github.com/stretchr/testify/require"
)

// execCmdML runs a tea.Cmd through the ModelListComponent's Update loop.
func execCmdML(t *testing.T, m *ModelListComponent, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		var next tea.Cmd
		_, next = m.Update(msg)
		cmd = next
	}
}

// readConfigJSON reads and unmarshals the JSON config file at path.
func readConfigJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	baseDir := filepath.Dir(path)
	fileName := filepath.Base(path)
	b, err := fs.ReadFile(os.DirFS(baseDir), fileName)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}

// readRecentModels reads the recent_models section from the config file.
func readRecentModels(t *testing.T, path string) map[string]any {
	t.Helper()
	out := readConfigJSON(t, path)
	rm, ok := out["recent_models"].(map[string]any)
	require.True(t, ok)
	return rm
}

func TestModelList_RecentlyUsedSectionAndPrunesInvalid(t *testing.T) {
	// Pre-initialize logger to os.DevNull to prevent file lock on Windows.
	log.Setup(os.DevNull, false)

	// Isolate config/data paths
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	// Pre-seed config so provider auto-update is disabled and we have recents
	confPath := filepath.Join(cfgDir, "crush", "crush.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(confPath), 0o755))
	initial := map[string]any{
		"options": map[string]any{
			"disable_provider_auto_update": true,
		},
		"models": map[string]any{
			"large": map[string]any{
				"model":    "m1",
				"provider": "p1",
			},
		},
		"recent_models": map[string]any{
			"large": []any{
				map[string]any{"model": "m2", "provider": "p1"},              // valid
				map[string]any{"model": "x", "provider": "unknown-provider"}, // invalid -> pruned
			},
		},
	}
	bts, err := json.Marshal(initial)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(confPath, bts, 0o644))

	// Also create empty providers.json to prevent loading real providers
	dataConfDir := filepath.Join(dataDir, "crush")
	require.NoError(t, os.MkdirAll(dataConfDir, 0o755))
	emptyProviders := []byte("[]")
	require.NoError(t, os.WriteFile(filepath.Join(dataConfDir, "providers.json"), emptyProviders, 0o644))

	// Initialize global config instance (no network due to auto-update disabled)
	_, err = config.Init(cfgDir, dataDir, false)
	require.NoError(t, err)

	// Build a small provider set for the list component
	provider := catwalk.Provider{
		ID:   catwalk.InferenceProvider("p1"),
		Name: "Provider One",
		Models: []catwalk.Model{
			{ID: "m1", Name: "Model One", DefaultMaxTokens: 100},
			{ID: "m2", Name: "Model Two", DefaultMaxTokens: 100}, // recent
		},
	}

	// Create and initialize the component with our provider set
	listKeyMap := list.DefaultKeyMap()
	cmp := NewModelListComponent(listKeyMap, "Find your fave", false)
	cmp.providers = []catwalk.Provider{provider}
	execCmdML(t, cmp, cmp.Init())

	// Find all recent items (IDs prefixed with "recent::") and verify pruning
	groups := cmp.list.Groups()
	require.NotEmpty(t, groups)
	var recentItems []list.CompletionItem[ModelOption]
	for _, g := range groups {
		for _, it := range g.Items {
			if strings.HasPrefix(it.ID(), "recent::") {
				recentItems = append(recentItems, it)
			}
		}
	}
	require.NotEmpty(t, recentItems, "no recent items found")
	// Ensure the valid recent (p1:m2) is present and the invalid one is not
	foundValid := false
	for _, it := range recentItems {
		if it.ID() == "recent::p1:m2" {
			foundValid = true
		}
		require.NotEqual(t, "recent::unknown-provider:x", it.ID(), "invalid recent should be pruned")
	}
	require.True(t, foundValid, "expected valid recent not found")

	// Verify original config in cfgDir remains unchanged
	origConfPath := filepath.Join(cfgDir, "crush", "crush.json")
	afterOrig, err := fs.ReadFile(os.DirFS(filepath.Dir(origConfPath)), filepath.Base(origConfPath))
	require.NoError(t, err)
	var origParsed map[string]any
	require.NoError(t, json.Unmarshal(afterOrig, &origParsed))
	origRM := origParsed["recent_models"].(map[string]any)
	origLarge := origRM["large"].([]any)
	require.Len(t, origLarge, 2, "original config should be unchanged")

	// Config should be rewritten with pruned recents in dataDir
	dataConf := filepath.Join(dataDir, "crush", "crush.json")
	rm := readRecentModels(t, dataConf)
	largeAny, ok := rm["large"].([]any)
	require.True(t, ok)
	// Ensure that only valid recent(s) remain and the invalid one is removed
	found := false
	for _, v := range largeAny {
		m := v.(map[string]any)
		require.NotEqual(t, "unknown-provider", m["provider"], "invalid provider should be pruned")
		if m["provider"] == "p1" && m["model"] == "m2" {
			found = true
		}
	}
	require.True(t, found, "persisted recents should include p1:m2")
}

func TestModelList_PrunesInvalidModelWithinValidProvider(t *testing.T) {
	// Pre-initialize logger to os.DevNull to prevent file lock on Windows.
	log.Setup(os.DevNull, false)

	// Isolate config/data paths
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	// Pre-seed config with valid provider but one invalid model
	confPath := filepath.Join(cfgDir, "crush", "crush.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(confPath), 0o755))
	initial := map[string]any{
		"options": map[string]any{
			"disable_provider_auto_update": true,
		},
		"models": map[string]any{
			"large": map[string]any{
				"model":    "m1",
				"provider": "p1",
			},
		},
		"recent_models": map[string]any{
			"large": []any{
				map[string]any{"model": "m1", "provider": "p1"},      // valid
				map[string]any{"model": "missing", "provider": "p1"}, // invalid model
			},
		},
	}
	bts, err := json.Marshal(initial)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(confPath, bts, 0o644))

	// Create empty providers.json
	dataConfDir := filepath.Join(dataDir, "crush")
	require.NoError(t, os.MkdirAll(dataConfDir, 0o755))
	emptyProviders := []byte("[]")
	require.NoError(t, os.WriteFile(filepath.Join(dataConfDir, "providers.json"), emptyProviders, 0o644))

	// Initialize global config instance
	_, err = config.Init(cfgDir, dataDir, false)
	require.NoError(t, err)

	// Build provider set that only includes m1, not "missing"
	provider := catwalk.Provider{
		ID:   catwalk.InferenceProvider("p1"),
		Name: "Provider One",
		Models: []catwalk.Model{
			{ID: "m1", Name: "Model One", DefaultMaxTokens: 100},
		},
	}

	// Create and initialize component
	listKeyMap := list.DefaultKeyMap()
	cmp := NewModelListComponent(listKeyMap, "Find your fave", false)
	cmp.providers = []catwalk.Provider{provider}
	execCmdML(t, cmp, cmp.Init())

	// Find all recent items
	groups := cmp.list.Groups()
	require.NotEmpty(t, groups)
	var recentItems []list.CompletionItem[ModelOption]
	for _, g := range groups {
		for _, it := range g.Items {
			if strings.HasPrefix(it.ID(), "recent::") {
				recentItems = append(recentItems, it)
			}
		}
	}
	require.NotEmpty(t, recentItems, "valid recent should exist")

	// Verify the valid recent is present and invalid model is not
	foundValid := false
	for _, it := range recentItems {
		if it.ID() == "recent::p1:m1" {
			foundValid = true
		}
		require.NotEqual(t, "recent::p1:missing", it.ID(), "invalid model should be pruned")
	}
	require.True(t, foundValid, "valid recent p1:m1 should be present")

	// Verify original config in cfgDir remains unchanged
	origConfPath := filepath.Join(cfgDir, "crush", "crush.json")
	afterOrig, err := fs.ReadFile(os.DirFS(filepath.Dir(origConfPath)), filepath.Base(origConfPath))
	require.NoError(t, err)
	var origParsed map[string]any
	require.NoError(t, json.Unmarshal(afterOrig, &origParsed))
	origRM := origParsed["recent_models"].(map[string]any)
	origLarge := origRM["large"].([]any)
	require.Len(t, origLarge, 2, "original config should be unchanged")

	// Config should be rewritten with pruned recents in dataDir
	dataConf := filepath.Join(dataDir, "crush", "crush.json")
	rm := readRecentModels(t, dataConf)
	largeAny, ok := rm["large"].([]any)
	require.True(t, ok)
	require.Len(t, largeAny, 1, "should only have one valid model")
	// Verify only p1:m1 remains
	m := largeAny[0].(map[string]any)
	require.Equal(t, "p1", m["provider"])
	require.Equal(t, "m1", m["model"])
}

func TestModelList_NoDuplicateModels(t *testing.T) {
	// Pre-initialize logger to os.DevNull to prevent file lock on Windows.
	log.Setup(os.DevNull, false)

	// Isolate config/data paths
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	// Pre-seed config with provider that should appear in both unknown and known sections
	confPath := filepath.Join(cfgDir, "crush", "crush.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(confPath), 0o755))
	initial := map[string]any{
		"options": map[string]any{
			"disable_provider_auto_update": true,
		},
		"models": map[string]any{
			"large": map[string]any{
				"model":    "gpt-oss-120b",
				"provider": "gpt-oss",
			},
		},
		"providers": map[string]any{
			"gpt-oss": map[string]any{
				"api_key": "$GPT_OSS_API_KEY",
				"models": []any{
					map[string]any{"id": "gpt-oss-120b", "name": "GPT OSS 120B"},
					map[string]any{"id": "qwen3-next", "name": "Qwen3 Next"},
				},
			},
		},
	}
	bts, err := json.Marshal(initial)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(confPath, bts, 0o644))

	// Create known providers list that includes gpt-oss
	dataConfDir := filepath.Join(dataDir, "crush")
	require.NoError(t, os.MkdirAll(dataConfDir, 0o755))
	knownProviders := []catwalk.Provider{
		{
			ID:   catwalk.InferenceProvider("gpt-oss"),
			Name: "GPT OSS",
			Models: []catwalk.Model{
				{ID: "gpt-oss-120b", Name: "GPT OSS 120B"},
				{ID: "qwen3-next", Name: "Qwen3 Next"},
				{ID: "devstral2", Name: "Devstral 2"},
			},
		},
	}
	providersJSON, _ := json.Marshal(knownProviders)
	require.NoError(t, os.WriteFile(filepath.Join(dataConfDir, "providers.json"), providersJSON, 0o644))

	// Initialize global config instance
	_, err = config.Init(cfgDir, dataDir, false)
	require.NoError(t, err)

	// Create and initialize component with known providers
	listKeyMap := list.DefaultKeyMap()
	cmp := NewModelListComponent(listKeyMap, "Find your fave", false)
	cmp.providers = knownProviders
	execCmdML(t, cmp, cmp.Init())

	// Get all groups and items to verify no duplicates
	groups := cmp.list.Groups()
	require.NotEmpty(t, groups)

	// Collect all item IDs to check for duplicates
	itemIDs := make(map[string]bool)
	var allItems []list.CompletionItem[ModelOption]

	for _, g := range groups {
		for _, item := range g.Items {
			// Skip recent items as they have different IDs (prefixed with "recent::")
			if !strings.HasPrefix(item.ID(), "recent::") {
				allItems = append(allItems, item)
				// For model items, check the underlying model ID
				modelID := item.Value().Model.ID
				providerID := string(item.Value().Provider.ID)
				fullID := providerID + ":" + modelID

				if itemIDs[fullID] {
					require.Fail(t, "Duplicate model found", "Model %s appears multiple times in the list", fullID)
				}
				itemIDs[fullID] = true
			}
		}
	}

	// Verify we found the expected models without duplicates
	require.NotEmpty(t, allItems, "Should have model items")

	// Check that at least some expected models are present
	foundGPTOSS := false
	foundQwen3 := false
	for itemID := range itemIDs {
		if strings.Contains(itemID, "gpt-oss:gpt-oss-120b") {
			foundGPTOSS = true
		}
		if strings.Contains(itemID, "gpt-oss:qwen3-next") {
			foundQwen3 = true
		}
	}

	require.True(t, foundGPTOSS, "Should find GPT OSS 120B model")
	require.True(t, foundQwen3, "Should find Qwen3 Next model")
}

func TestModelList_LinuxXDGPaths(t *testing.T) {
	// Pre-initialize logger to os.DevNull to prevent file lock on Windows.
	log.Setup(os.DevNull, false)

	// Isolate config/data paths
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	// Simulate Linux XDG paths with overlapping provider configurations
	confPath := filepath.Join(cfgDir, "crush", "crush.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(confPath), 0o755))

	// Create overlapping provider configs (simulating Linux path conflicts)
	initial := map[string]any{
		"options": map[string]any{
			"disable_provider_auto_update": true,
		},
		"models": map[string]any{
			"large": map[string]any{
				"model":    "gpt-oss-120b",
				"provider": "gpt-oss",
			},
		},
		"providers": map[string]any{
			"gpt-oss": map[string]any{
				"api_key": "$GPT_OSS_API_KEY",
				"models": []any{
					map[string]any{"id": "gpt-oss-120b", "name": "GPT OSS 120B"},
					map[string]any{"id": "qwen3-next", "name": "Qwen3 Next"},
					map[string]any{"id": "devstral2", "name": "Devstral 2"},
				},
			},
		},
		"recent_models": map[string]any{
			"large": []any{
				map[string]any{"model": "gpt-oss-120b", "provider": "gpt-oss"},
			},
		},
	}
	bts, err := json.Marshal(initial)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(confPath, bts, 0o644))

	// Create data directory with provider cache (Linux typical scenario)
	dataConfDir := filepath.Join(dataDir, "crush")
	require.NoError(t, os.MkdirAll(dataConfDir, 0o755))

	// Create known providers list that overlaps with config providers
	knownProviders := []catwalk.Provider{
		{
			ID:   catwalk.InferenceProvider("gpt-oss"),
			Name: "GPT OSS",
			Models: []catwalk.Model{
				{ID: "gpt-oss-120b", Name: "GPT OSS 120B"},
				{ID: "qwen3-next", Name: "Qwen3 Next"},
				{ID: "devstral2", Name: "Devstral 2"},
				{ID: "intellect3", Name: "Intellect 3"},
			},
		},
		{
			ID:   catwalk.InferenceProvider("llama.cpp"),
			Name: "Llama.cpp",
			Models: []catwalk.Model{
				{ID: "gpt-oss-120b", Name: "GPT OSS 120B"},
				{ID: "qwen3-next", Name: "Qwen3 Next"},
			},
		},
	}
	providersJSON, _ := json.Marshal(knownProviders)
	require.NoError(t, os.WriteFile(filepath.Join(dataConfDir, "providers.json"), providersJSON, 0o644))

	// Initialize global config instance
	_, err = config.Init(cfgDir, dataDir, false)
	require.NoError(t, err)

	// Create and initialize component with known providers
	listKeyMap := list.DefaultKeyMap()
	cmp := NewModelListComponent(listKeyMap, "Find your fave", false)
	cmp.providers = knownProviders
	execCmdML(t, cmp, cmp.Init())

	// Get all groups and items to verify no duplicates
	groups := cmp.list.Groups()
	require.NotEmpty(t, groups)

	// Collect all item IDs to check for duplicates across all sources
	modelCounts := make(map[string]int)
	var allItems []list.CompletionItem[ModelOption]

	for _, g := range groups {
		for _, item := range g.Items {
			if !strings.HasPrefix(item.ID(), "recent::") {
				allItems = append(allItems, item)
				modelID := item.Value().Model.ID
				providerID := string(item.Value().Provider.ID)
				fullID := providerID + ":" + modelID
				modelCounts[fullID]++
			}
		}
	}

	// Verify no duplicate models across all groups
	require.NotEmpty(t, allItems, "Should have model items")

	// Check that no model appears more than once
	for modelID, count := range modelCounts {
		require.Equal(t, 1, count, "Model %s should appear exactly once, found %d times", modelID, count)
	}

	// Specifically check the overlapping models
	require.Equal(t, 1, modelCounts["gpt-oss:gpt-oss-120b"], "GPT OSS 120B should appear exactly once")
	require.Equal(t, 1, modelCounts["gpt-oss:qwen3-next"], "Qwen3 Next should appear exactly once")
}

func TestModelKey_EmptyInputs(t *testing.T) {
	// Empty provider
	require.Equal(t, "", modelKey("", "model"))
	// Empty model
	require.Equal(t, "", modelKey("provider", ""))
	// Both empty
	require.Equal(t, "", modelKey("", ""))
	// Valid inputs
	require.Equal(t, "p:m", modelKey("p", "m"))
}

func TestModelList_AllRecentsInvalid(t *testing.T) {
	// Pre-initialize logger to os.DevNull to prevent file lock on Windows.
	log.Setup(os.DevNull, false)

	// Isolate config/data paths
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	// Pre-seed config with only invalid recents
	confPath := filepath.Join(cfgDir, "crush", "crush.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(confPath), 0o755))
	initial := map[string]any{
		"options": map[string]any{
			"disable_provider_auto_update": true,
		},
		"models": map[string]any{
			"large": map[string]any{
				"model":    "m1",
				"provider": "p1",
			},
		},
		"recent_models": map[string]any{
			"large": []any{
				map[string]any{"model": "x", "provider": "unknown1"},
				map[string]any{"model": "y", "provider": "unknown2"},
			},
		},
	}
	bts, err := json.Marshal(initial)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(confPath, bts, 0o644))

	// Also create empty providers.json and data config
	dataConfDir := filepath.Join(dataDir, "crush")
	require.NoError(t, os.MkdirAll(dataConfDir, 0o755))
	emptyProviders := []byte("[]")
	require.NoError(t, os.WriteFile(filepath.Join(dataConfDir, "providers.json"), emptyProviders, 0o644))

	// Initialize global config instance with isolated dataDir
	_, err = config.Init(cfgDir, dataDir, false)
	require.NoError(t, err)

	// Build provider set (doesn't include unknown1 or unknown2)
	provider := catwalk.Provider{
		ID:   catwalk.InferenceProvider("p1"),
		Name: "Provider One",
		Models: []catwalk.Model{
			{ID: "m1", Name: "Model One", DefaultMaxTokens: 100},
		},
	}

	// Create and initialize component
	listKeyMap := list.DefaultKeyMap()
	cmp := NewModelListComponent(listKeyMap, "Find your fave", false)
	cmp.providers = []catwalk.Provider{provider}
	execCmdML(t, cmp, cmp.Init())

	// Verify no recent items exist in UI
	groups := cmp.list.Groups()
	require.NotEmpty(t, groups)
	var recentItems []list.CompletionItem[ModelOption]
	for _, g := range groups {
		for _, it := range g.Items {
			if strings.HasPrefix(it.ID(), "recent::") {
				recentItems = append(recentItems, it)
			}
		}
	}
	require.Empty(t, recentItems, "all invalid recents should be pruned, resulting in no recent section")

	// Verify original config in cfgDir remains unchanged
	origConfPath := filepath.Join(cfgDir, "crush", "crush.json")
	afterOrig, err := fs.ReadFile(os.DirFS(filepath.Dir(origConfPath)), filepath.Base(origConfPath))
	require.NoError(t, err)
	var origParsed map[string]any
	require.NoError(t, json.Unmarshal(afterOrig, &origParsed))
	origRM := origParsed["recent_models"].(map[string]any)
	origLarge := origRM["large"].([]any)
	require.Len(t, origLarge, 2, "original config should be unchanged")

	// Config should be rewritten with empty recents in dataDir
	dataConf := filepath.Join(dataDir, "crush", "crush.json")
	rm := readRecentModels(t, dataConf)
	// When all recents are pruned, the value may be nil or an empty array
	largeVal := rm["large"]
	if largeVal == nil {
		// nil is acceptable - means empty
		return
	}
	largeAny, ok := largeVal.([]any)
	require.True(t, ok, "large key should be nil or array")
	require.Empty(t, largeAny, "persisted recents should be empty after pruning all invalid entries")
}
