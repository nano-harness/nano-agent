package merger

import (
	"fmt"
	"reflect"
	"strings"
)

// MergeStrategy defines how a field should be merged
type MergeStrategy string

const (
	// StrategyReplace means higher layer completely replaces lower layer (default)
	StrategyReplace MergeStrategy = "replace"
	// StrategyMergeByKey means merge lists by a key field (upsert semantics)
	StrategyMergeByKey MergeStrategy = "merge_by_key"
	// StrategyAppend means concatenate lists and deduplicate
	StrategyAppend MergeStrategy = "append"
)

// MergePolicy defines how to merge a specific field
type MergePolicy struct {
	Strategy MergeStrategy
	KeyField string // Used for merge_by_key strategy
}

// Merge performs a deep merge of multiple configuration layers.
// Layers are provided from lowest to highest priority (e.g., defaults, user, project, managed, env).
// The policy map defines merge strategies for specific field paths.
// Returns a merged map suitable for unmarshaling into the target config struct.
func Merge(layers []map[string]interface{}, policies map[string]MergePolicy) (map[string]interface{}, error) {
	if len(layers) == 0 {
		return make(map[string]interface{}), nil
	}

	result := make(map[string]interface{})

	for _, layer := range layers {
		if err := mergeInto(result, layer, "", policies); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// mergeInto recursively merges src into dst based on the merge policies
func mergeInto(dst, src map[string]interface{}, path string, policies map[string]MergePolicy) error {
	for key, srcValue := range src {
		fieldPath := key
		if path != "" {
			fieldPath = path + "." + key
		}

		// Get merge policy for this field (default is replace)
		policy := policies[fieldPath]
		if policy.Strategy == "" {
			policy.Strategy = StrategyReplace
		}

		// Validate strategy
		if policy.Strategy != StrategyReplace && policy.Strategy != StrategyAppend && policy.Strategy != StrategyMergeByKey {
			return fmt.Errorf("unknown merge strategy %q at %s", policy.Strategy, fieldPath)
		}

		// Handle explicit nil/null - this clears the field
		if srcValue == nil {
			dst[key] = nil
			continue
		}

		dstValue, exists := dst[key]

		// If destination doesn't exist or is nil, validate and copy
		if !exists || dstValue == nil {
			// For merge_by_key, validate items even on initial copy
			if policy.Strategy == StrategyMergeByKey {
				if policy.KeyField == "" {
					return fmt.Errorf("merge_by_key strategy requires key_field at %s", fieldPath)
				}
				if err := validateMergeByKeyItems(srcValue, policy.KeyField); err != nil {
					return fmt.Errorf("failed to validate items at %s: %w", fieldPath, err)
				}
			}
			dst[key] = deepCopy(srcValue)
			continue
		}

		// Both exist - apply merge strategy
		switch policy.Strategy {
		case StrategyReplace:
			// If both are maps AND there are child policies for this path, recursively merge
			dstMap, dstIsMap := dstValue.(map[string]interface{})
			srcMap, srcIsMap := srcValue.(map[string]interface{})
			if dstIsMap && srcIsMap && hasChildPolicies(fieldPath, policies) {
				// Recursively merge nested maps (allows nested policies to work)
				if err := mergeInto(dstMap, srcMap, fieldPath, policies); err != nil {
					return err
				}
				dst[key] = dstMap
			} else {
				// Replace entirely for non-maps or when no child policies exist
				dst[key] = deepCopy(srcValue)
			}

		case StrategyAppend:
			// Append and deduplicate lists
			merged, err := appendLists(dstValue, srcValue)
			if err != nil {
				return fmt.Errorf("failed to append lists at %s: %w", fieldPath, err)
			}
			dst[key] = merged

		case StrategyMergeByKey:
			// Merge lists by key field
			if policy.KeyField == "" {
				return fmt.Errorf("merge_by_key strategy requires key_field at %s", fieldPath)
			}
			merged, err := mergeListsByKey(dstValue, srcValue, policy.KeyField, fieldPath, policies)
			if err != nil {
				return fmt.Errorf("failed to merge lists by key at %s: %w", fieldPath, err)
			}
			dst[key] = merged
		}
	}

	return nil
}

// appendLists concatenates two lists and removes duplicates
func appendLists(dst, src interface{}) (interface{}, error) {
	dstSlice, ok := dst.([]interface{})
	if !ok {
		// Try to convert other slice types
		dstSlice = toInterfaceSlice(dst)
		if dstSlice == nil {
			return nil, fmt.Errorf("destination is not a slice: %T", dst)
		}
	}

	srcSlice, ok := src.([]interface{})
	if !ok {
		srcSlice = toInterfaceSlice(src)
		if srcSlice == nil {
			return nil, fmt.Errorf("source is not a slice: %T", src)
		}
	}

	// Handle explicit empty array as a clear operation
	if len(srcSlice) == 0 && isExplicitEmpty(src) {
		return []interface{}{}, nil
	}

	// Append and deduplicate
	seen := make(map[interface{}]bool)
	result := make([]interface{}, 0, len(dstSlice)+len(srcSlice))

	for _, item := range dstSlice {
		key := makeDedupeKey(item)
		if !seen[key] {
			seen[key] = true
			result = append(result, deepCopy(item))
		}
	}

	for _, item := range srcSlice {
		key := makeDedupeKey(item)
		if !seen[key] {
			seen[key] = true
			result = append(result, deepCopy(item))
		}
	}

	return result, nil
}

// mergeListsByKey merges two lists by matching items on a key field
func mergeListsByKey(dst, src interface{}, keyField, path string, policies map[string]MergePolicy) (interface{}, error) {
	dstSlice, ok := dst.([]interface{})
	if !ok {
		dstSlice = toInterfaceSlice(dst)
		if dstSlice == nil {
			return nil, fmt.Errorf("destination is not a slice: %T", dst)
		}
	}

	srcSlice, ok := src.([]interface{})
	if !ok {
		srcSlice = toInterfaceSlice(src)
		if srcSlice == nil {
			return nil, fmt.Errorf("source is not a slice: %T", src)
		}
	}

	// Handle explicit empty array as a clear operation
	if len(srcSlice) == 0 && isExplicitEmpty(src) {
		return []interface{}{}, nil
	}

	// Build a map of existing items by key
	itemsByKey := make(map[interface{}]map[string]interface{})
	order := make([]interface{}, 0, len(dstSlice))

	for _, item := range dstSlice {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			itemMap = toStringInterfaceMap(item)
			if itemMap == nil {
				return nil, fmt.Errorf("list item is not a map: %T", item)
			}
		}

		keyValue, ok := itemMap[keyField]
		if !ok {
			return nil, fmt.Errorf("key field %q not found in item", keyField)
		}

		if _, exists := itemsByKey[keyValue]; !exists {
			order = append(order, keyValue)
		}
		itemsByKey[keyValue] = deepCopyMap(itemMap)
	}

	// Merge or append items from source
	for _, item := range srcSlice {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			itemMap = toStringInterfaceMap(item)
			if itemMap == nil {
				return nil, fmt.Errorf("list item is not a map: %T", item)
			}
		}

		keyValue, ok := itemMap[keyField]
		if !ok {
			return nil, fmt.Errorf("key field %q not found in item", keyField)
		}

		if existing, exists := itemsByKey[keyValue]; exists {
			// Merge recursively into existing item
			if err := mergeInto(existing, itemMap, path, policies); err != nil {
				return nil, err
			}
		} else {
			// New item - append to end
			order = append(order, keyValue)
			itemsByKey[keyValue] = deepCopyMap(itemMap)
		}
	}

	// Build result in original order
	result := make([]interface{}, 0, len(order))
	for _, key := range order {
		result = append(result, itemsByKey[key])
	}

	return result, nil
}

// deepCopy creates a deep copy of an interface{} value
func deepCopy(v interface{}) interface{} {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case map[string]interface{}:
		return deepCopyMap(val)
	case []interface{}:
		return deepCopySlice(val)
	default:
		// Primitive types are copied by value
		return val
	}
}

// deepCopyMap creates a deep copy of a map
func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = deepCopy(v)
	}
	return result
}

// deepCopySlice creates a deep copy of a slice
func deepCopySlice(s []interface{}) []interface{} {
	result := make([]interface{}, len(s))
	for i, v := range s {
		result[i] = deepCopy(v)
	}
	return result
}

// toInterfaceSlice attempts to convert various slice types to []interface{}
func toInterfaceSlice(v interface{}) []interface{} {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Slice {
		return nil
	}

	result := make([]interface{}, val.Len())
	for i := 0; i < val.Len(); i++ {
		result[i] = val.Index(i).Interface()
	}
	return result
}

// toStringInterfaceMap attempts to convert a map to map[string]interface{}
func toStringInterfaceMap(v interface{}) map[string]interface{} {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Map {
		return nil
	}

	result := make(map[string]interface{})
	iter := val.MapRange()
	for iter.Next() {
		key := iter.Key()
		if key.Kind() == reflect.String {
			result[key.String()] = iter.Value().Interface()
		}
	}
	return result
}

// makeDedupeKey creates a key for deduplication
func makeDedupeKey(v interface{}) interface{} {
	// For simple types, use the value itself
	switch val := v.(type) {
	case string, int, int64, float64, bool:
		return val
	case map[string]interface{}:
		// For maps, we could hash the entire map, but that's complex
		// For now, just return the map itself (won't dedupe maps)
		return val
	default:
		return val
	}
}

// isExplicitEmpty checks if a value is an explicitly set empty array
// In YAML unmarshaling, an explicit [] becomes an empty slice, while
// an omitted field doesn't create the key at all
func isExplicitEmpty(v interface{}) bool {
	// If we got here, the value exists in the map
	// An empty slice that exists is explicit
	if slice, ok := v.([]interface{}); ok {
		return len(slice) == 0
	}
	return false
}

// validateMergeByKeyItems validates that all items in a slice have the required key field
func validateMergeByKeyItems(v interface{}, keyField string) error {
	slice, ok := v.([]interface{})
	if !ok {
		slice = toInterfaceSlice(v)
		if slice == nil {
			return fmt.Errorf("value is not a slice: %T", v)
		}
	}

	for _, item := range slice {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			itemMap = toStringInterfaceMap(item)
			if itemMap == nil {
				return fmt.Errorf("list item is not a map: %T", item)
			}
		}

		if _, ok := itemMap[keyField]; !ok {
			return fmt.Errorf("key field %q not found in item", keyField)
		}
	}

	return nil
}

// hasChildPolicies checks if there are any policies defined for children of the given path
func hasChildPolicies(parentPath string, policies map[string]MergePolicy) bool {
	prefix := parentPath + "."
	for policyPath := range policies {
		if strings.HasPrefix(policyPath, prefix) {
			return true
		}
	}
	return false
}
