package outbound

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// reportWireTools checks the emitted definitions, including schemas after
// provider cleanup. It cannot claim preservation for a tool the builder drops.
func reportWireTools(input conversionInput, typ OutboundType) LossReport {
	tools, _ := input.wire["tools"].([]any)
	var report LossReport
	for index, tool := range input.request.Tools {
		kind := strings.ToLower(strings.TrimSpace(tool.Type))
		var parameters any
		found := false
		for _, value := range tools {
			wire, _ := value.(map[string]any)
			if wire == nil {
				continue
			}
			if typ == OutboundTypeGemini {
				native := map[string]string{"server_search": "googleSearch", "code_execution": "codeExecution", "url_context": "urlContext"}[kind]
				if native != "" && wire[native] != nil {
					found = true
				}
				declarations, _ := wire["functionDeclarations"].([]any)
				for _, value := range declarations {
					declaration, _ := value.(map[string]any)
					if (kind == "function" || kind == "") && declaration["name"] == tool.Function.Name {
						found, parameters = true, declaration["parameters"]
					}
				}
				continue
			}
			wireKind, _ := wire["type"].(string)
			if kind != "function" && kind != "" {
				if wireKind == kind {
					found = true
				}
				continue
			}
			name, schema := wire["name"], wire["parameters"]
			if typ == OutboundTypeOpenAIChat {
				function, _ := wire["function"].(map[string]any)
				name, schema = function["name"], function["parameters"]
			} else if typ == OutboundTypeAnthropic {
				schema = wire["input_schema"]
			}
			if name == tool.Function.Name {
				found, parameters = true, schema
			}
		}
		if !found {
			report = append(report, CapabilityLoss{Field: fmt.Sprintf("tools[%d].type", index), Action: LossActionDrop, Reason: fmt.Sprintf("tool type %q is not representable on %s", tool.Type, typ)})
			continue
		}
		if (kind == "function" || kind == "") && len(tool.Function.Parameters) > 0 {
			var source any
			if err := json.Unmarshal(tool.Function.Parameters, &source); err != nil {
				continue
			}
			if typ == OutboundTypeGemini {
				normalizeSchemaTypeCase(source)
				normalizeSchemaTypeCase(parameters)
			}
			if !reflect.DeepEqual(source, parameters) {
				report = append(report, CapabilityLoss{Field: fmt.Sprintf("tools[%d].function.parameters", index), Action: LossActionTranslate, Reason: fmt.Sprintf("%s rewrites tool schema constraints in the emitted definition", typ)})
			}
		}
	}
	return report
}

func normalizeSchemaTypeCase(value any) {
	switch node := value.(type) {
	case map[string]any:
		if typ, ok := node["type"].(string); ok {
			node["type"] = strings.ToLower(typ)
		}
		for _, child := range node {
			normalizeSchemaTypeCase(child)
		}
	case []any:
		for _, child := range node {
			normalizeSchemaTypeCase(child)
		}
	}
}
