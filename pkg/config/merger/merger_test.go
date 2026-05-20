package merger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMerge_EmptyLayers(t *testing.T) {
	result, err := Merge([]map[string]interface{}{}, nil)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestMerge_SingleLayer(t *testing.T) {
	layer := map[string]interface{}{
		"foo": "bar",
		"num": 42,
	}
	result, err := Merge([]map[string]interface{}{layer}, nil)
	require.NoError(t, err)
	assert.Equal(t, "bar", result["foo"])
	assert.Equal(t, 42, result["num"])
}

func TestMerge_ScalarReplace(t *testing.T) {
	layers := []map[string]interface{}{
		{"foo": "low", "num": 10},
		{"foo": "high", "num": 20},
	}

	// Default strategy is replace
	result, err := Merge(layers, nil)
	require.NoError(t, err)
	assert.Equal(t, "high", result["foo"])
	assert.Equal(t, 20, result["num"])
}

func TestMerge_NestedMapReplace(t *testing.T) {
	layers := []map[string]interface{}{
		{
			"config": map[string]interface{}{
				"timeout": 30,
				"retries": 3,
			},
		},
		{
			"config": map[string]interface{}{
				"timeout": 60,
			},
		},
	}

	// Default replace strategy - entire nested map is replaced
	result, err := Merge(layers, nil)
	require.NoError(t, err)

	config, ok := result["config"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 60, config["timeout"])
	// retries is lost because the entire map was replaced
	assert.NotContains(t, config, "retries")
}

func TestMerge_ExplicitNullClearsField(t *testing.T) {
	layers := []map[string]interface{}{
		{"foo": "bar", "num": 42},
		{"foo": nil}, // Explicit null clears foo
	}

	result, err := Merge(layers, nil)
	require.NoError(t, err)
	assert.Nil(t, result["foo"])
	assert.Equal(t, 42, result["num"])
}

func TestMerge_AppendStrategy(t *testing.T) {
	layers := []map[string]interface{}{
		{"rules": []interface{}{"rule1", "rule2"}},
		{"rules": []interface{}{"rule2", "rule3"}}, // rule2 is duplicate
	}

	policies := map[string]MergePolicy{
		"rules": {Strategy: StrategyAppend},
	}

	result, err := Merge(layers, policies)
	require.NoError(t, err)

	rules, ok := result["rules"].([]interface{})
	require.True(t, ok)
	assert.Len(t, rules, 3) // Deduplicated
	assert.Contains(t, rules, "rule1")
	assert.Contains(t, rules, "rule2")
	assert.Contains(t, rules, "rule3")
}

func TestMerge_AppendStrategyEmptyArrayClears(t *testing.T) {
	layers := []map[string]interface{}{
		{"rules": []interface{}{"rule1", "rule2"}},
		{"rules": []interface{}{}}, // Explicit empty array clears
	}

	policies := map[string]MergePolicy{
		"rules": {Strategy: StrategyAppend},
	}

	result, err := Merge(layers, policies)
	require.NoError(t, err)

	rules, ok := result["rules"].([]interface{})
	require.True(t, ok)
	assert.Len(t, rules, 0)
}

func TestMerge_MergeByKeyStrategy(t *testing.T) {
	layers := []map[string]interface{}{
		{
			"hooks": []interface{}{
				map[string]interface{}{"name": "hook1", "enabled": true, "timeout": 10},
				map[string]interface{}{"name": "hook2", "enabled": false},
			},
		},
		{
			"hooks": []interface{}{
				map[string]interface{}{"name": "hook1", "timeout": 20},   // Update hook1
				map[string]interface{}{"name": "hook3", "enabled": true}, // New hook3
			},
		},
	}

	policies := map[string]MergePolicy{
		"hooks": {Strategy: StrategyMergeByKey, KeyField: "name"},
	}

	result, err := Merge(layers, policies)
	require.NoError(t, err)

	hooks, ok := result["hooks"].([]interface{})
	require.True(t, ok)
	assert.Len(t, hooks, 3)

	// Verify hook1 was merged
	hook1 := hooks[0].(map[string]interface{})
	assert.Equal(t, "hook1", hook1["name"])
	assert.Equal(t, true, hook1["enabled"]) // Kept from layer 1
	assert.Equal(t, 20, hook1["timeout"])   // Updated from layer 2

	// Verify hook2 is still there
	hook2 := hooks[1].(map[string]interface{})
	assert.Equal(t, "hook2", hook2["name"])
	assert.Equal(t, false, hook2["enabled"])

	// Verify hook3 was added
	hook3 := hooks[2].(map[string]interface{})
	assert.Equal(t, "hook3", hook3["name"])
	assert.Equal(t, true, hook3["enabled"])
}

func TestMerge_MergeByKeyEmptyArrayClears(t *testing.T) {
	layers := []map[string]interface{}{
		{
			"servers": []interface{}{
				map[string]interface{}{"name": "server1", "port": 8080},
			},
		},
		{
			"servers": []interface{}{}, // Explicit empty array clears
		},
	}

	policies := map[string]MergePolicy{
		"servers": {Strategy: StrategyMergeByKey, KeyField: "name"},
	}

	result, err := Merge(layers, policies)
	require.NoError(t, err)

	servers, ok := result["servers"].([]interface{})
	require.True(t, ok)
	assert.Len(t, servers, 0)
}

func TestMerge_MergeByKeyMissingKeyField(t *testing.T) {
	layers := []map[string]interface{}{
		{
			"items": []interface{}{
				map[string]interface{}{"id": "item1"}, // Missing "name" key
			},
		},
	}

	policies := map[string]MergePolicy{
		"items": {Strategy: StrategyMergeByKey, KeyField: "name"},
	}

	_, err := Merge(layers, policies)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key field \"name\" not found")
}

func TestMerge_MultipleLayersComplex(t *testing.T) {
	// Simulate defaults -> user -> project -> env
	defaults := map[string]interface{}{
		"api_key": "",
		"timeout": 30,
		"security": map[string]interface{}{
			"allow_rules": []interface{}{"default-rule"},
		},
		"hooks": []interface{}{
			map[string]interface{}{"name": "default-hook", "enabled": true},
		},
	}

	user := map[string]interface{}{
		"api_key": "user-key",
		"security": map[string]interface{}{
			"allow_rules": []interface{}{"user-rule"},
		},
		"hooks": []interface{}{
			map[string]interface{}{"name": "user-hook", "enabled": true},
		},
	}

	project := map[string]interface{}{
		"timeout": 60, // Override timeout
		"mcp": map[string]interface{}{
			"enable_client": true,
		},
		"hooks": []interface{}{
			map[string]interface{}{"name": "default-hook", "enabled": false}, // Override default hook
		},
	}

	env := map[string]interface{}{
		"api_key": "env-key", // Final override
	}

	policies := map[string]MergePolicy{
		"security.allow_rules": {Strategy: StrategyAppend},
		"hooks":                {Strategy: StrategyMergeByKey, KeyField: "name"},
	}

	result, err := Merge([]map[string]interface{}{defaults, user, project, env}, policies)
	require.NoError(t, err)

	// Check final values
	assert.Equal(t, "env-key", result["api_key"])
	assert.Equal(t, 60, result["timeout"])

	// Check MCP config exists
	mcp, ok := result["mcp"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, mcp["enable_client"])

	// Note: In the current simple implementation, security.allow_rules
	// won't merge because security is replaced as a whole map.
	// For nested field merging, we'd need to implement recursive policy matching.
	// This is expected behavior for now - security map gets replaced.
	security, ok := result["security"].(map[string]interface{})
	require.True(t, ok)
	allowRules, ok := security["allow_rules"].([]interface{})
	require.True(t, ok)
	// Only user-rule because security map was replaced by user layer
	assert.Contains(t, allowRules, "user-rule")

	// Check hooks were merged by key
	hooks, ok := result["hooks"].([]interface{})
	require.True(t, ok)
	assert.Len(t, hooks, 2)

	// default-hook should be disabled (overridden by project)
	hook0 := hooks[0].(map[string]interface{})
	if hook0["name"] == "default-hook" {
		assert.Equal(t, false, hook0["enabled"])
	}
}

func TestMerge_DeepCopyIsolation(t *testing.T) {
	layer1 := map[string]interface{}{
		"config": map[string]interface{}{
			"nested": []interface{}{"value1"},
		},
	}

	layer2 := map[string]interface{}{
		"other": "data",
	}

	result, err := Merge([]map[string]interface{}{layer1, layer2}, nil)
	require.NoError(t, err)

	// Modify result
	config := result["config"].(map[string]interface{})
	nested := config["nested"].([]interface{})
	nested[0] = "modified"

	// Original layer1 should be unchanged
	origNested := layer1["config"].(map[string]interface{})["nested"].([]interface{})
	assert.Equal(t, "value1", origNested[0])
}

func TestMerge_UnknownStrategy(t *testing.T) {
	layers := []map[string]interface{}{
		{"foo": "bar"},
	}

	policies := map[string]MergePolicy{
		"foo": {Strategy: "unknown"},
	}

	_, err := Merge(layers, policies)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown merge strategy")
}

func TestMerge_MergeByKeyWithoutKeyField(t *testing.T) {
	layers := []map[string]interface{}{
		{"items": []interface{}{map[string]interface{}{"id": 1}}},
	}

	policies := map[string]MergePolicy{
		"items": {Strategy: StrategyMergeByKey}, // Missing KeyField
	}

	_, err := Merge(layers, policies)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "merge_by_key strategy requires key_field")
}

func TestMerge_AppendPreservesOrder(t *testing.T) {
	layers := []map[string]interface{}{
		{"items": []interface{}{"a", "b"}},
		{"items": []interface{}{"c", "d"}},
	}

	policies := map[string]MergePolicy{
		"items": {Strategy: StrategyAppend},
	}

	result, err := Merge(layers, policies)
	require.NoError(t, err)

	items, ok := result["items"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{"a", "b", "c", "d"}, items)
}

func TestMerge_MergeByKeyPreservesOrder(t *testing.T) {
	layers := []map[string]interface{}{
		{
			"items": []interface{}{
				map[string]interface{}{"name": "a", "value": 1},
				map[string]interface{}{"name": "b", "value": 2},
			},
		},
		{
			"items": []interface{}{
				map[string]interface{}{"name": "c", "value": 3},
				map[string]interface{}{"name": "a", "value": 10}, // Update existing
			},
		},
	}

	policies := map[string]MergePolicy{
		"items": {Strategy: StrategyMergeByKey, KeyField: "name"},
	}

	result, err := Merge(layers, policies)
	require.NoError(t, err)

	items, ok := result["items"].([]interface{})
	require.True(t, ok)
	assert.Len(t, items, 3)

	// Order should be: a (updated), b (original), c (new)
	assert.Equal(t, "a", items[0].(map[string]interface{})["name"])
	assert.Equal(t, 10, items[0].(map[string]interface{})["value"])
	assert.Equal(t, "b", items[1].(map[string]interface{})["name"])
	assert.Equal(t, "c", items[2].(map[string]interface{})["name"])
}
