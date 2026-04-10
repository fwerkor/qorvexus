package tool

func schemaObject(properties map[string]any, required ...string) map[string]any {
	out := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func schemaString(description string) map[string]any {
	return schemaPrimitive("string", description)
}

func schemaInteger(description string) map[string]any {
	return schemaPrimitive("integer", description)
}

func schemaBoolean(description string) map[string]any {
	return schemaPrimitive("boolean", description)
}

func schemaNumber(description string) map[string]any {
	return schemaPrimitive("number", description)
}

func schemaStringEnum(description string, values ...string) map[string]any {
	out := schemaString(description)
	out["enum"] = values
	return out
}

func schemaArray(description string, items map[string]any) map[string]any {
	out := map[string]any{
		"type":  "array",
		"items": items,
	}
	if description != "" {
		out["description"] = description
	}
	return out
}

func schemaPrimitive(kind string, description string) map[string]any {
	out := map[string]any{"type": kind}
	if description != "" {
		out["description"] = description
	}
	return out
}
