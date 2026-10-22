package agenttools

import (
	"encoding/json"
	"fmt"
)

// NativeDefinitions describes only executor capabilities. Workflow instructions
// remain in the editable workspace, never in a hidden runtime prompt.
func NativeDefinitions(allowed func(string) bool) []any {
	out := []any{}
	for _, d := range Definitions {
		if !allowed(d.Name) {
			continue
		}
		properties := map[string]any{}
		required := []string{}
		for _, p := range d.Parameters {
			s := map[string]any{"type": p.Type, "description": p.Description}
			if p.Type == "array" {
				s["items"] = map[string]any{"type": "object"}
				if d.Name == "submit_decisions" {
					s["items"] = decisionSchema()
				}
			}
			properties[p.Name] = s
			if p.Required {
				required = append(required, p.Name)
			}
		}
		out = append(out, map[string]any{"type": "function", "function": map[string]any{
			"name": d.Name, "description": d.Description, "parameters": map[string]any{
				"type": "object", "properties": properties, "required": required, "additionalProperties": false,
			},
		}})
	}
	return out
}

func decisionSchema() map[string]any {
	p := map[string]any{}
	for _, name := range []string{"symbol", "reasoning"} {
		p[name] = map[string]any{"type": "string"}
	}
	p["action"] = map[string]any{"type": "string", "enum": []string{"open_long", "open_short", "close_long", "close_short", "hold"}}
	for _, name := range []string{"leverage", "confidence"} {
		p[name] = map[string]any{"type": "integer"}
	}
	for _, name := range []string{"position_size_usd", "stop_loss", "take_profit"} {
		p[name] = map[string]any{"type": "number"}
	}
	return map[string]any{"type": "object", "properties": p, "required": []string{"symbol", "action"}, "additionalProperties": false}
}

// ValidateNative rejects malformed arguments before an executor (especially a
// mutation) can interpret a partial object or silently use omitted defaults.
func ValidateNative(name, arguments string) error {
	var def *Definition
	for i := range Definitions {
		if Definitions[i].Name == name {
			def = &Definitions[i]
			break
		}
	}
	if def == nil {
		return fmt.Errorf("unknown_tool: %s", name)
	}
	var args map[string]json.RawMessage
	if json.Unmarshal([]byte(arguments), &args) != nil || args == nil {
		return fmt.Errorf("invalid_arguments: expected one JSON object")
	}
	for key := range args {
		found := false
		for _, p := range def.Parameters {
			if p.Name == key {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("invalid_arguments: unknown field %s", key)
		}
	}
	for _, p := range def.Parameters {
		raw, ok := args[p.Name]
		if !ok {
			if p.Required {
				return fmt.Errorf("invalid_arguments: missing %s", p.Name)
			}
			continue
		}
		if string(raw) == "null" {
			return fmt.Errorf("invalid_arguments: %s cannot be null", p.Name)
		}
		var target any
		switch p.Type {
		case "string":
			target = new(string)
		case "integer":
			target = new(int64)
		case "number":
			target = new(float64)
		case "array":
			target = new([]json.RawMessage)
		default:
			return fmt.Errorf("invalid_arguments: unsupported type %s", p.Type)
		}
		if json.Unmarshal(raw, target) != nil {
			return fmt.Errorf("invalid_arguments: %s must be %s", p.Name, p.Type)
		}
	}
	if name == "submit_decisions" {
		var rows []map[string]json.RawMessage
		if err := json.Unmarshal(args["decisions"], &rows); err != nil {
			return fmt.Errorf("invalid_arguments: decisions must contain objects")
		}
		schema := decisionSchema()["properties"].(map[string]any)
		for _, row := range rows {
			if row == nil {
				return fmt.Errorf("invalid_arguments: decision cannot be null")
			}
			for _, key := range []string{"symbol", "action"} {
				var value string
				if json.Unmarshal(row[key], &value) != nil || value == "" {
					return fmt.Errorf("invalid_arguments: missing decision %s", key)
				}
			}
			for key, raw := range row {
				property, ok := schema[key]
				if !ok {
					return fmt.Errorf("invalid_arguments: unknown decision field %s", key)
				}
				kind := property.(map[string]any)["type"]
				var target any
				switch kind {
				case "string":
					target = new(string)
				case "integer":
					target = new(int64)
				case "number":
					target = new(float64)
				}
				if string(raw) == "null" || json.Unmarshal(raw, target) != nil {
					return fmt.Errorf("invalid_arguments: decision %s must be %s", key, kind)
				}
			}
			var action string
			json.Unmarshal(row["action"], &action)
			if action != "open_long" && action != "open_short" && action != "close_long" && action != "close_short" && action != "hold" {
				return fmt.Errorf("invalid_arguments: unknown decision action %s", action)
			}
		}
	}
	return nil
}
