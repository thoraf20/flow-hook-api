package transform

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dop251/goja"
	"github.com/itchyny/gojq"
)

// ExecuteTransformation executes a transformation script based on the language
func ExecuteTransformation(language, script string, input interface{}) (interface{}, error) {
	// Convert input to JSON if it's a string
	var inputData interface{}
	if inputStr, ok := input.(string); ok {
		if err := json.Unmarshal([]byte(inputStr), &inputData); err != nil {
			// If it's not valid JSON, treat as plain string
			inputData = inputStr
		}
	} else {
		inputData = input
	}

	switch strings.ToLower(language) {
	case "javascript", "js":
		return executeJavaScript(script, inputData)
	case "jq":
		return executeJQ(script, inputData)
	case "jsonata":
		return executeJSONata(script, inputData)
	default:
		return nil, fmt.Errorf("unsupported transformation language: %s", language)
	}
}

// executeJavaScript executes JavaScript transformation using goja
func executeJavaScript(script string, input interface{}) (interface{}, error) {
	vm := goja.New()

	// Convert input to JSON string for JavaScript context
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}

	// Set up the JavaScript environment
	vm.Set("input", input)
	vm.Set("data", input)
	
	// Parse input JSON string for JSON.parse() usage
	vm.Set("inputJSON", string(inputJSON))

	// Wrap the script intelligently
	// Users can provide:
	// 1. An expression: input.field
	// 2. A function: function(data) { return data.field; }
	// 3. An arrow function: (data) => data.field
	// 4. A function call: transform(input)
	wrappedScript := script
	scriptTrimmed := strings.TrimSpace(script)
	
	// Check if it's already a complete statement/expression
	hasReturn := strings.Contains(scriptTrimmed, "return")
	hasArrow := strings.Contains(scriptTrimmed, "=>")
	hasFunction := strings.Contains(scriptTrimmed, "function")
	
	if !hasReturn && !hasArrow && !hasFunction {
		// It's a simple expression, wrap it to return the result
		wrappedScript = fmt.Sprintf("(function() { return %s; })()", scriptTrimmed)
	} else if hasFunction && !strings.Contains(scriptTrimmed, "(") && !strings.Contains(scriptTrimmed, ")") {
		// Incomplete function, wrap it
		wrappedScript = fmt.Sprintf("(function() { %s })()", scriptTrimmed)
	} else if hasFunction && !strings.Contains(scriptTrimmed, "input") && !strings.Contains(scriptTrimmed, "data") {
		// Function that doesn't reference input, call it with input
		wrappedScript = fmt.Sprintf("(%s)(input)", scriptTrimmed)
	}

	// Execute the script
	value, err := vm.RunString(wrappedScript)
	if err != nil {
		return nil, fmt.Errorf("JavaScript execution error: %w", err)
	}

	// Convert result to Go value
	result := value.Export()
	return result, nil
}

// executeJQ executes JQ transformation using gojq
func executeJQ(query string, input interface{}) (interface{}, error) {
	// Parse the JQ query
	jqQuery, err := gojq.Parse(query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JQ query: %w", err)
	}

	// Execute the query
	iter := jqQuery.Run(input)
	
	var results []interface{}
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return nil, fmt.Errorf("JQ execution error: %w", err)
		}
		results = append(results, v)
	}

	// If single result, return it directly; otherwise return array
	if len(results) == 1 {
		return results[0], nil
	}
	return results, nil
}

// executeJSONata executes JSONata transformation
// Note: Pure Go JSONata implementation is limited, so we'll use a simplified approach
// For production, you might want to use a CGO wrapper or shell out to node-jsonata
func executeJSONata(expression string, input interface{}) (interface{}, error) {
	// Convert input to JSON
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}

	// For now, we'll use JavaScript engine to execute JSONata-like expressions
	// This is a simplified implementation - for full JSONata support, consider:
	// 1. Using a CGO wrapper around libjsonata
	// 2. Shelling out to node-jsonata
	// 3. Using a Go port if available
	
	// Basic JSONata support using JavaScript engine
	// JSONata expressions are similar to JavaScript but with different syntax
	// We'll translate common JSONata patterns to JavaScript
	
	vm := goja.New()
	vm.Set("input", input)
	vm.Set("data", input)
	vm.Set("inputJSON", string(inputJSON))

	// Simple JSONata to JavaScript translation for common patterns
	jsScript := translateJSONataToJS(expression)
	
	value, err := vm.RunString(jsScript)
	if err != nil {
		return nil, fmt.Errorf("JSONata execution error: %w", err)
	}

	result := value.Export()
	return result, nil
}

// translateJSONataToJS translates basic JSONata expressions to JavaScript
// This is a simplified translator - full JSONata support would require a proper parser
func translateJSONataToJS(expression string) string {
	// Remove leading/trailing whitespace
	expr := strings.TrimSpace(expression)
	
	// Handle common JSONata patterns
	// $ - root context (maps to input/data)
	expr = strings.ReplaceAll(expr, "$", "input")
	
	// @ - current context (maps to current value in iteration)
	// This is more complex and would need proper parsing
	
	// Basic property access (already works in JS)
	// Wrap in a function that returns the result
	return fmt.Sprintf("(function() { return %s; })()", expr)
}

