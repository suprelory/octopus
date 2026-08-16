package relay

import (
	"encoding/json"
	"fmt"
	"strings"
)

func requiredJSONModel(payload []byte) (string, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope == nil {
		if err == nil {
			err = fmt.Errorf("request body must be a JSON object")
		}
		return "", err
	}
	rawModel, ok := envelope["model"]
	if !ok {
		return "", fmt.Errorf("request body requires model")
	}
	var modelName string
	if err := json.Unmarshal(rawModel, &modelName); err != nil || strings.TrimSpace(modelName) == "" {
		return "", fmt.Errorf("request body contains an invalid model")
	}
	return strings.TrimSpace(modelName), nil
}

func jsonPayloadHasTopLevelModel(payload []byte) bool {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope == nil {
		return false
	}
	_, ok := envelope["model"]
	return ok
}

func replaceRequiredJSONModel(payload []byte, modelName string) ([]byte, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil, fmt.Errorf("mapped model is empty")
	}
	currentModel, err := requiredJSONModel(payload)
	if err != nil {
		return nil, err
	}
	if currentModel == modelName {
		return payload, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, err
	}
	encodedModel, err := json.Marshal(modelName)
	if err != nil {
		return nil, err
	}
	envelope["model"] = encodedModel
	return json.Marshal(envelope)
}
