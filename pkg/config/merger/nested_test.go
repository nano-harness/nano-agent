package merger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMerge_NestedPolicies(t *testing.T) {
	layers := []map[string]interface{}{
		{
			"security": map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{"name": "hook1", "enabled": true},
					map[string]interface{}{"name": "hook2", "enabled": true},
				},
				"allow_rules": []interface{}{"rule1"},
			},
		},
		{
			"security": map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{"name": "hook1", "enabled": false}, // Override
					map[string]interface{}{"name": "hook3", "enabled": true},  // New
				},
				"allow_rules": []interface{}{"rule2"},
			},
		},
	}

	policies := map[string]MergePolicy{
		"security.hooks":       {Strategy: StrategyMergeByKey, KeyField: "name"},
		"security.allow_rules": {Strategy: StrategyAppend},
	}

	result, err := Merge(layers, policies)
	require.NoError(t, err)

	security, ok := result["security"].(map[string]interface{})
	require.True(t, ok)

	hooks, ok := security["hooks"].([]interface{})
	require.True(t, ok)
	assert.Len(t, hooks, 3) // hook1 (updated), hook2, hook3

	allowRules, ok := security["allow_rules"].([]interface{})
	require.True(t, ok)
	assert.Len(t, allowRules, 2) // rule1, rule2
	assert.Contains(t, allowRules, "rule1")
	assert.Contains(t, allowRules, "rule2")
}
