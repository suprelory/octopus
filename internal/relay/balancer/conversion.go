package balancer

import "github.com/bestruirui/octopus/internal/model"

func cloneRequestConversion(value *model.RequestConversion) *model.RequestConversion {
	if value == nil || value.Mode == "" {
		return nil
	}
	copy := *value
	return &copy
}
